/**
 * TsToHls Dashboard Core Logic
 * Version: 1.2.2
 * Optimized for local deployment and fixed drag-and-drop bug.
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
    // 每3秒更新一次资源占用
    setInterval(checkStatus, 3000);
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

    art = new Artplayer({
        container: container,
        url: `/stream/${ch.id}/index.m3u8`,
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
                    const hls = new Hls();
                    hls.loadSource(url);
                    hls.attachMedia(video);
                } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
                    video.src = url;
                }
            },
        },
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
        if(!input.files[0]) {
            alert("请先选择 M3U 文件");
            return;
        }
        
        uploadBtn.disabled = true;
        uploadBtn.textContent = "正在处理...";
        
        const fd = new FormData();
        fd.append('m3uFile', input.files[0]);

        try {
            const res = await fetch('/api/upload', { method: 'POST', body: fd });
            if(res.ok) {
                location.reload();
            } else {
                alert("上传失败，请检查文件格式");
            }
        } catch (e) {
            alert("请求出错");
        } finally {
            uploadBtn.disabled = false;
            uploadBtn.textContent = "上传并转换";
        }
    };
}

document.getElementById('copyBtn').onclick = () => {
    const url = document.getElementById('m3uUrl').value;
    navigator.clipboard.writeText(url).then(() => {
        const btn = document.getElementById('copyBtn');
        const oldText = btn.textContent;
        btn.textContent = "已复制";
        btn.classList.replace('bg-slate-900', 'bg-emerald-600');
        setTimeout(() => {
            btn.textContent = oldText;
            btn.classList.replace('bg-emerald-600', 'bg-slate-900');
        }, 2000);
    });
};

window.onload = init;