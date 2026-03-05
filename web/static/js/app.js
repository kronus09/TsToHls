/**
 * TsToHls Dashboard Core Logic
 * Version: 1.2.2
 * Optimized for local deployment, fixed drag-and-drop bug and clipboard compatibility.
 */

let channels = [];
let currentGroup = ''; 
let art = null;
let isExpertMode = false;

const init = () => {
    // 初始化图标
    if (window.lucide) {
        lucide.createIcons();
    }
    setupTabs();
    setupDragAndDrop();
    loadListFromServer();
    loadConfigFromServer();
    checkSourceFile();
    // 每3秒更新一次资源占用
    setInterval(checkStatus, 3000);
};

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
    const pageC = document.getElementById('consolePage');
    const pageP = document.getElementById('previewPage');

    const switchTab = (toConsole) => {
        btnC.className = toConsole ? "tab-btn active" : "tab-btn inactive";
        btnP.className = !toConsole ? "tab-btn active" : "tab-btn inactive";
        pageC.classList.toggle('hidden', !toConsole);
        pageP.classList.toggle('hidden', toConsole);

        if (!toConsole) {
            // 预览页显示时重新计算播放器尺寸
            setTimeout(() => { if(art) art.resize(); }, 150);
            renderPreview();
        }
    };

    btnC.onclick = () => switchTab(true);
    btnP.onclick = () => switchTab(false);
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
        btn.textContent = g;
        btn.onclick = () => {
            currentGroup = g;
            renderPreview();
        };
        gc.appendChild(btn);
    });

    grid.innerHTML = '';
    const filtered = currentGroup === '全部' ? channels : channels.filter(c => c.group === currentGroup);
    
    filtered.forEach(ch => {
        const b = document.createElement('div');
        b.className = 'channel-btn'; 
        b.innerHTML = `
            <img src="${ch.logo || '/static/logo.png'}" onerror="this.src='/static/logo.png'">
            <span>${ch.name}</span>
        `;
        b.onclick = () => playStream(ch);
        grid.appendChild(b);
    });
};

const playStream = (ch) => {
    const container = document.getElementById('playerContainer');
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
                            const hls = new Hls({
                                xhrSetup: function(xhr, url) {
                                    xhr.withCredentials = true;
                                },
                                enableWorker: false,
                                lowLatencyMode: true,
                                fastStart: true,
                                maxBufferLength: 1,
                                maxMaxBufferLength: 3,
                                maxBufferHole: 0.5,
                                nudgeMaxRetry: 3,
                                nudgeRetryDelay: 100,
                                nudgeMaxDelay: 1000
                            });
                            hls.loadSource(url);
                            hls.attachMedia(video);
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

        if (channels.length) {
            document.getElementById('m3uUrl').value = `${window.location.origin}/playlist/tstohls.m3u`;
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
                if (!Array.from(el.options).some(o => o.value === val)) {
                    el.add(new Option(val, val));
                }
                el.value = val;
            }
        });
    } catch (e) {}
}

document.getElementById('expertModeBtn').onclick = () => {
    isExpertMode = !isExpertMode;
    const inputs = document.querySelectorAll('#configForm select');
    inputs.forEach(i => i.disabled = !isExpertMode);
    
    document.getElementById('configActions').classList.toggle('hidden', !isExpertMode);
    document.getElementById('expertModeBtn').textContent = isExpertMode ? "取消修改" : "编辑配置";
};

document.getElementById('saveConfigBtn').onclick = async () => {
    const fd = new FormData(document.getElementById('configForm'));
    const data = Object.fromEntries(fd.entries());
    const numKeys = ['max_processes', 'hls_time', 'hls_list_size', 'idle_timeout', 'reconnect_delay'];
    numKeys.forEach(k => { if(data[k]) data[k] = parseInt(data[k]); });

    try {
        const res = await fetch('/api/config', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(data)
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

    uploadBtn.onclick = async () => {
        const urlInput = document.getElementById('urlInput');
        const url = urlInput.value.trim();
        
        uploadBtn.disabled = true;
        uploadBtn.textContent = "正在处理...";
        
        try {
            let res;
            const checkSourceReliability = document.getElementById('checkSourceReliability').checked;
            
            // 检查是否有新选择的文件或输入的URL
            if(input.files[0]) {
                // 有新选择的文件，直接上传
                const fd = new FormData();
                fd.append('m3uFile', input.files[0]);
                fd.append('checkSourceReliability', checkSourceReliability);
                res = await fetch('/api/upload', { method: 'POST', body: fd });
            } else if(url) {
                // 有输入的URL，直接上传
                res = await fetch('/api/upload', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify({ url: url, checkSourceReliability: checkSourceReliability })
                });
            } else {
                // 没有新选择的文件或输入的URL，检查source.m3u文件
                const sourceCheck = await fetch('/api/check-source');
                const sourceData = await sourceCheck.json();
                
                if(sourceData.exists) {
                    if(sourceData.sourceUrl) {
                        // 文件内有订阅地址，从订阅地址重新拉取
                        res = await fetch('/api/upload', {
                            method: 'POST',
                            headers: {
                                'Content-Type': 'application/json'
                            },
                            body: JSON.stringify({ url: sourceData.sourceUrl, checkSourceReliability: checkSourceReliability })
                        });
                    } else {
                        // 文件内没有订阅地址，直接使用该文件
                        // 这里需要创建一个FormData对象，模拟文件上传
                        // 但由于我们无法直接读取本地文件，需要通过后端API来处理
                        res = await fetch('/api/reprocess', {
                            method: 'POST',
                            headers: {
                                'Content-Type': 'application/json'
                            },
                            body: JSON.stringify({ checkSourceReliability: checkSourceReliability })
                        });
                    }
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

window.onload = init;