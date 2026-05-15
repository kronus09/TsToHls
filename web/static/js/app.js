/**
 * TsToHls Dashboard Core Logic
 * Version: 1.4.0
 * Optimized for local deployment, fixed drag-and-drop bug and clipboard compatibility.
 */

let channels = [];
let currentGroup = ''; 
let art = null;
let currentHls = null;
let isExpertMode = false;

const init = () => {
    if (window.lucide) {
        lucide.createIcons();
    }
    checkMigration().then(() => {
        setupTabs();
        setupDragAndDrop();
        document.getElementById('m3uUrl').value = `${window.location.origin}/playlist/tstohls.m3u`;
        loadListFromServer();
        loadConfigFromServer();
        checkSourceFile();
        checkFluva();
        checkStatus();
        setInterval(checkStatus, 30000);
    });

    document.getElementById('checkSourceReliability').addEventListener('change', async (e) => {
        try {
            const res = await fetch('/api/config', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({ check_source_reliability: e.target.checked })
            });
            if (!res.ok) e.target.checked = !e.target.checked;
        } catch (err) {
            e.target.checked = !e.target.checked;
        }
    });
};

async function checkMigration() {
    try {
        const r = await fetch('/api/status');
        const d = await r.json();
        if (d.migration_status === 'migrating') {
            const overlay = document.createElement('div');
            overlay.id = 'migrationOverlay';
            overlay.style.cssText = 'position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(15,23,42,0.95);z-index:99999;display:flex;flex-direction:column;align-items:center;justify-content:center;';
            overlay.innerHTML = `
                <div style="text-align:center;">
                    <div style="width:48px;height:48px;border:4px solid #e2e8f0;border-top-color:#4f46e5;border-radius:50%;animation:spin 1s linear infinite;margin:0 auto 24px;"></div>
                    <p style="color:#f8fafc;font-size:18px;font-weight:700;margin-bottom:12px;">系统升级中</p>
                    <p style="color:#94a3b8;font-size:14px;">正在转换历史数据，请稍候...</p>
                </div>
                <style>@keyframes spin{to{transform:rotate(360deg)}}</style>
            `;
            document.body.appendChild(overlay);

            while (true) {
                await new Promise(r => setTimeout(r, 2000));
                const rr = await fetch('/api/status');
                const dd = await rr.json();
                if (dd.migration_status !== 'migrating') {
                    const el = document.getElementById('migrationOverlay');
                    if (el) el.remove();
                    break;
                }
            }
        }
    } catch (e) {}
}

// 检查source.m3u文件是否存在，并提取订阅地址
    const checkSourceFile = () => {
        fetch('/api/check-source')
            .then(response => response.json())
            .then(data => {
                if (data.exists) {
                    // 文件存在，显示文件状态
                    const dropZoneContent = document.getElementById('dropZoneContent');
                    dropZoneContent.innerHTML = `
                        <i data-lucide="file-check" class="w-10 h-10 text-green-500 mx-auto mb-4"></i>
                        <p class="text-xs font-bold text-slate-600">上次上传的 m3u 文件</p>
                    `;
                    lucide.createIcons();
                }
                
                // 显示订阅地址
                if (data.sourceUrl) {
                    document.getElementById('urlInput').value = data.sourceUrl;
                }
            })
            .catch(error => {
                console.error('检查source.m3u文件失败:', error);
            });
    };

const setupTabs = () => {
    const btnC = document.getElementById('tabConsole');
    const btnP = document.getElementById('tabPreview');
    const btnD = document.getElementById('tabDirectTs');
    const btnU = document.getElementById('tabUsage');
    const pageC = document.getElementById('consolePage');
    const pageP = document.getElementById('previewPage');
    const pageD = document.getElementById('directTsPage');
    const pageU = document.getElementById('usagePage');

    const allBtns = [btnC, btnP, btnD, btnU];
    const allPages = [pageC, pageP, pageD, pageU];

    const switchTab = (index) => {
        allBtns.forEach((btn, i) => {
            btn.className = i === index ? "tab-btn active text-sm px-3 py-1.5" : "tab-btn inactive text-sm px-3 py-1.5";
        });
        allPages.forEach((page, i) => {
            page.classList.toggle('hidden', i !== index);
        });

        if (index === 1) {
            setTimeout(() => { if(art) art.resize(); }, 150);
            renderPreview();
        }
    };

    btnC.onclick = () => switchTab(0);
    btnP.onclick = () => switchTab(1);
    btnD.onclick = () => switchTab(2);
    btnU.onclick = () => switchTab(3);

    const urlParams = new URLSearchParams(window.location.search);
    if (urlParams.get('tab') === 'preview') {
        switchTab(1);
    } else if (urlParams.get('tab') === 'directts') {
        switchTab(2);
    } else if (urlParams.get('tab') === 'usage') {
        switchTab(3);
    }
};

const renderPreview = () => {
    const gc = document.getElementById('groupContainer');
    const grid = document.getElementById('channelGrid');
    if (!channels.length || !gc || !grid) return;

    let groups = [];
    channels.forEach(c => {
        if (c.group && !groups.includes(c.group)) {
            groups.push(c.group);
        }
    });
    
    if (!currentGroup && groups.length > 0) {
        currentGroup = groups[0];
    }
    
    const displayGroups = [...groups, '全部'];

    gc.innerHTML = '';
    displayGroups.forEach(g => {
        const btn = document.createElement('button');
        btn.className = `group-tag ${currentGroup === g ? 'active' : ''}`;
        const count = g === '全部' ? channels.length : channels.filter(c => c.group === g).length;
        btn.innerHTML = `${g}<span class="group-count">${count}</span>`;
        btn.onclick = () => {
            currentGroup = g;
            renderPreview();
        };
        gc.appendChild(btn);
    });

    grid.innerHTML = '';
    const filtered = currentGroup === '全部' ? channels : channels.filter(c => c.group === currentGroup);
    
    filtered.forEach(ch => {
        if (!ch.enabled) return;
        const b = document.createElement('div');
        b.className = 'channel-btn'; 
        b.innerHTML = `
            <img src="${ch.logo || '/static/logos/logo.png'}" onerror="this.src='/static/logos/logo.png'">
            <span>${ch.name}</span>
        `;
        b.onclick = () => playStream(ch);
        grid.appendChild(b);
    });
};

const playStream = (ch) => {
    const container = document.getElementById('playerContainer');
    
    if (currentHls) {
        currentHls.destroy();
        currentHls = null;
    }
    if (art) art.destroy(true);
    container.innerHTML = '';

    // 从tstohls.m3u文件中获取播放链接
    fetch('/playlist/tstohls.m3u')
        .then(response => response.text())
        .then(content => {
            // 查找包含当前频道ID的播放链接
            const lines = content.split('\n');
            let streamUrl = '';
            for (let i = 0; i < lines.length; i++) {
                if (lines[i].includes(ch.id)) {
                    // 找到频道对应的播放链接
                    streamUrl = lines[i].trim();
                    break;
                }
            }
            
            console.log('播放URL:', streamUrl);
            
            if (!streamUrl) {
                container.innerHTML = '<p class="text-slate-400 text-center text-sm py-8">该频道未启用，无法播放</p>';
                return;
            }
            
            art = new Artplayer({
                container: container,
                url: streamUrl,
                isLive: true,
                autoplay: true,
                theme: '#4f46e5',
                fullscreen: true,
                playbackRate: true,
                aspectRatio: true,
                setting: true,
                customType: {
                    m3u8: function (video, url) {
                        if (window.Hls && Hls.isSupported()) {
                            currentHls = new Hls({
                                xhrSetup: function(xhr, url) {
                                    xhr.withCredentials = true;
                                },
                                enableWorker: true,
                                lowLatencyMode: true,
                                liveSyncDurationCount: 0.5,
                                liveMaxLatencyDurationCount: 3,
                                maxBufferLength: 1,
                                maxMaxBufferLength: 3,
                                maxBufferHole: 0.5,
                            });
                            currentHls.loadSource(url);
                            currentHls.attachMedia(video);
                        } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
                            video.src = url;
                            video.crossOrigin = 'use-credentials';
                        }
                    },
                },
            });
        })
        .catch(error => {
            console.error('获取播放链接失败:', error);
            container.innerHTML = '<p class="text-red-500 text-center">获取播放链接失败，请重试</p>';
        });
};

async function loadListFromServer() {
    try {
        const res = await fetch('/api/list?t=' + Date.now());
        const data = await res.json();
        channels = Array.isArray(data) ? data : (data.channels || []);
        
        if (document.getElementById('channelCount')) {
            document.getElementById('channelCount').textContent = channels.length;
        }
        if (document.getElementById('enabledCount')) {
            const enabled = channels.filter(c => c.enabled !== false).length;
            document.getElementById('enabledCount').textContent = enabled;
        }

        if (channels.length) {
            renderPreview();
        }
    } catch (e) {
        console.error("加载列表失败", e);
    }
}

async function checkStatus() {
    try {
        const r = await fetch('/api/status?t=' + Date.now());
        const d = await r.json();
        if(document.getElementById('processCount')) document.getElementById('processCount').textContent = d.active_count;
        if(document.getElementById('cpuUsage')) document.getElementById('cpuUsage').textContent = d.cpu || '0';
        if(document.getElementById('memUsage')) document.getElementById('memUsage').textContent = d.mem || '0';
    } catch(e) {}
}

async function loadConfigFromServer() {
    try {
        const res = await fetch('/api/config');
        const config = await res.json();
        const form = document.getElementById('configForm');
        
        Object.keys(config).forEach(key => {
            const el = form.querySelector(`[name="${key}"]`);
            if (el) {
                const val = String(config[key]);
                if (el.tagName === 'SELECT') {
                    if (!Array.from(el.options).some(o => o.value === val)) {
                        el.add(new Option(val, val));
                    }
                    el.value = val;
                } else if (el.tagName === 'INPUT') {
                    el.value = val;
                }
            }
        });
    } catch (e) {}
}

document.getElementById('expertModeBtn').onclick = () => {
    isExpertMode = !isExpertMode;
    const inputs = document.querySelectorAll('#configForm select, #configForm input');
    inputs.forEach(i => i.disabled = !isExpertMode);
    
    document.getElementById('configActions').classList.toggle('hidden', !isExpertMode);
    document.getElementById('expertModeBtn').textContent = isExpertMode ? "取消修改" : "编辑配置";
};
document.getElementById('saveConfigBtn').onclick = async () => {
    try {
        const fd = new FormData(document.getElementById('configForm'));
        const formData = Object.fromEntries(fd.entries());
        
        const numKeys = ['max_processes', 'hls_time', 'hls_list_size', 'idle_timeout', 'reconnect_delay'];
        numKeys.forEach(k => { if(formData[k]) formData[k] = parseInt(formData[k]); });
        
        formData.check_source_reliability = document.getElementById('checkSourceReliability').checked;

        const res = await fetch('/api/config', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(formData)
        });
        if(res.ok) {
            alert("配置已更新，服务将重启应用新参数");
            location.reload();
        }
    } catch (e) {
        alert("保存失败");
    }
};

document.getElementById('resetConfigBtn').onclick = async () => {
    if(confirm("确定要恢复到出厂默认设置吗？")) {
        await fetch('/api/config?action=reset', { method: 'POST' });
        location.reload();
    }
};

function setupDragAndDrop() {
    const zone = document.getElementById('dropZone');
    const input = document.getElementById('fileInput');
    const uploadBtn = document.getElementById('uploadBtn');
    
    if(!zone || !input) return;

    // 修复 Bug: 阻止浏览器默认打开文件的行为
    ['dragenter', 'dragover', 'dragleave', 'drop'].forEach(eventName => {
        zone.addEventListener(eventName, (e) => {
            e.preventDefault();
            e.stopPropagation();
        }, false);
    });

    zone.onclick = () => input.click();

    // 拖拽进入样式变化
    zone.addEventListener('dragover', () => zone.classList.add('bg-indigo-50'), false);
    zone.addEventListener('dragleave', () => zone.classList.remove('bg-indigo-50'), false);

    // 核心修复: 处理拖拽放下的文件
    zone.addEventListener('drop', (e) => {
        zone.classList.remove('bg-indigo-50');
        const dt = e.dataTransfer;
        const files = dt.files;
        if (files.length > 0) {
            input.files = files; // 将拖拽的文件赋值给 input
            handleFileSelect(files[0]);
        }
    }, false);

    input.onchange = () => {
        if(input.files[0]) {
            handleFileSelect(input.files[0]);
        }
    };

    function handleFileSelect(file) {
        document.getElementById('dropZoneContent').innerHTML = `
            <i data-lucide="check-circle" class="w-10 h-10 text-emerald-500 mx-auto mb-4"></i>
            <p class="text-xs font-bold text-indigo-600">已选择: ${file.name}</p>
        `;
        if(window.lucide) lucide.createIcons();
    }

    let urlEditedByUser = false;
    const urlInput = document.getElementById('urlInput');
    urlInput.addEventListener('input', () => { urlEditedByUser = true; });

    uploadBtn.onclick = async () => {
        const url = urlInput.value.trim();
        
        uploadBtn.disabled = true;
        uploadBtn.textContent = "正在处理...";
        
        try {
            let res;
            const checkSourceReliability = document.getElementById('checkSourceReliability').checked;
            
            if(input.files[0]) {
                const fd = new FormData();
                fd.append('m3uFile', input.files[0]);
                fd.append('checkSourceReliability', checkSourceReliability);
                res = await fetch('/api/upload', { method: 'POST', body: fd });
            } else if(url && urlEditedByUser) {
                res = await fetch('/api/upload', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ url: url, checkSourceReliability: checkSourceReliability })
                });
            } else {
                const sourceCheck = await fetch('/api/check-source');
                const sourceData = await sourceCheck.json();
                
                if(sourceData.exists && sourceData.sourceUrl) {
                    res = await fetch('/api/upload', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ url: sourceData.sourceUrl, checkSourceReliability: checkSourceReliability })
                    });
                } else if(sourceData.exists) {
                    res = await fetch('/api/reprocess', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ checkSourceReliability: checkSourceReliability })
                    });
                } else {
                    alert("请选择 M3U 文件或输入订阅地址");
                    uploadBtn.disabled = false;
                    uploadBtn.textContent = "上传并转换";
                    return;
                }
            }
            
            if(res.ok) {
                location.reload();
            } else {
                alert("处理失败，请检查文件格式或URL是否正确");
            }
        } catch (e) {
            alert("请求出错");
        } finally {
            uploadBtn.disabled = false;
            uploadBtn.textContent = "上传并转换";
        }
    };
}

// 核心修复: 兼容 HTTP/HTTPS/IP 访问的复制逻辑
document.getElementById('copyBtn').onclick = () => {
    const url = document.getElementById('m3uUrl').value;
    const btn = document.getElementById('copyBtn');

    const copyToClipboard = (text) => {
        // 如果是 HTTPS 或 localhost，使用现代 API
        if (navigator.clipboard && window.isSecureContext) {
            return navigator.clipboard.writeText(text);
        } else {
            // 否则使用隐藏 Textarea 方案兼容 IP 直接访问
            return new Promise((resolve, reject) => {
                const textArea = document.createElement("textarea");
                textArea.value = text;
                textArea.style.position = "fixed";
                textArea.style.left = "-9999px";
                textArea.style.top = "0";
                document.body.appendChild(textArea);
                textArea.focus();
                textArea.select();
                try {
                    const successful = document.execCommand('copy');
                    document.body.removeChild(textArea);
                    successful ? resolve() : reject();
                } catch (err) {
                    document.body.removeChild(textArea);
                    reject(err);
                }
            });
        }
    };

    copyToClipboard(url).then(() => {
        const oldText = btn.textContent;
        btn.textContent = "已复制";
        btn.classList.replace('bg-slate-900', 'bg-emerald-600');
        setTimeout(() => {
            btn.textContent = oldText;
            btn.classList.replace('bg-emerald-600', 'bg-slate-900');
        }, 2000);
    }).catch(err => {
        console.error('复制失败:', err);
    });
};

let fluvaOnline = false;
let fluvaAddr = '';

async function checkFluva(url) {
    const statusEl = document.getElementById('fluvaStatus');
    const pushBtn = document.getElementById('fluvaPushBtn');
    const urlInput = document.getElementById('fluvaUrl');
    const checkUrl = url || '';

    try {
        const res = await fetch('/api/fluva/check', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url: checkUrl })
        });
        const data = await res.json();

        if (data.online) {
            fluvaOnline = true;
            fluvaAddr = data.url;
            const peer = data.peer || {};
            statusEl.innerHTML = `已连接 <strong class="text-indigo-600">${peer.name || 'Fluva'}</strong> v${peer.version || '?'} <span class="text-emerald-500">●</span>`;
            pushBtn.disabled = false;
            urlInput.classList.add('hidden');
        } else {
            fluvaOnline = false;
            fluvaAddr = data.url;
            statusEl.innerHTML = '未找到，请输入地址';
            urlInput.classList.remove('hidden');
            urlInput.value = data.url || '';
            pushBtn.disabled = true;
        }
    } catch (e) {
        statusEl.innerHTML = '检测失败';
        pushBtn.disabled = true;
    }
}

document.getElementById('fluvaUrl').addEventListener('change', function() {
    checkFluva(this.value.trim());
});

document.getElementById('fluvaPushBtn').onclick = async function() {
    const btn = this;
    const statusEl = document.getElementById('fluvaStatus');
    btn.disabled = true;
    btn.textContent = '推送中...';

    try {
        const res = await fetch('/api/fluva/push', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url: fluvaAddr })
        });
        const data = await res.json();

        if (data.ok) {
            statusEl.innerHTML = `已推送 <span class="text-emerald-500">●</span> <span class="font-mono">${data.hls_playlist_url}</span>`;
            btn.textContent = '已推送';
            setTimeout(() => { btn.textContent = '推送'; btn.disabled = false; }, 3000);
        } else {
            statusEl.innerHTML = `推送失败: ${data.error}`;
            btn.textContent = '推送';
            btn.disabled = false;
        }
    } catch (e) {
        statusEl.innerHTML = '推送请求失败';
        btn.textContent = '推送';
        btn.disabled = false;
    }
};

window.onload = init;