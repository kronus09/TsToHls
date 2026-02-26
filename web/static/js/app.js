let channels = [];
let currentGroup = ''; 
let artPlayer = null;

function init() {
    const ft = document.getElementById('footerTemplate').innerHTML;
    document.querySelectorAll('.footer-target').forEach(el => el.innerHTML = ft);
    lucide.createIcons();
    setupDragAndDrop();
    loadListFromServer();
}

async function loadListFromServer() {
    try {
        const res = await fetch('/api/list?cache_bust=' + Date.now());
        if (!res.ok) throw new Error("服务器响应异常");
        const rawData = await res.json();
        let list = Array.isArray(rawData) ? rawData : (rawData.channels || []);
        if (list.length > 0) {
            channels = list;
            updateUI(channels.length);
            return true;
        }
    } catch (e) { console.error("同步失败:", e); }
    return false;
}

function setupDragAndDrop() {
    const fileInput = document.getElementById('fileInput');
    const dropZone = document.getElementById('dropZone');
    dropZone.addEventListener('dragover', (e) => {
        e.preventDefault();
        dropZone.classList.add('border-indigo-500', 'bg-indigo-50');
    });
    ['dragleave', 'drop'].forEach(n => {
        dropZone.addEventListener(n, () => dropZone.classList.remove('border-indigo-500', 'bg-indigo-50'));
    });
    dropZone.addEventListener('drop', (e) => {
        e.preventDefault();
        if (e.dataTransfer.files.length > 0) {
            fileInput.files = e.dataTransfer.files;
            handleFileSelect(e.dataTransfer.files[0].name);
        }
    });
    dropZone.onclick = () => fileInput.click();
    fileInput.onchange = () => { if(fileInput.files[0]) handleFileSelect(fileInput.files[0].name); };
}

function handleFileSelect(name) {
    document.getElementById('dropZoneContent').innerHTML = `
        <i data-lucide="check-circle" class="w-12 h-12 text-emerald-500 mx-auto mb-4"></i>
        <p class="text-indigo-600 font-bold">已选: ${name}</p>
    `;
    lucide.createIcons();
}

document.getElementById('uploadBtn').onclick = async () => {
    const fileInput = document.getElementById('fileInput');
    if(!fileInput.files[0]) return fileInput.click();
    const btn = document.getElementById('uploadBtn');
    btn.textContent = "正在转换..."; btn.disabled = true;
    const fd = new FormData(); fd.append('m3uFile', fileInput.files[0]);
    try {
        const res = await fetch('/api/upload', { method: 'POST', body: fd });
        if (res.ok) {
            await new Promise(r => setTimeout(r, 2000));
            await loadListFromServer();
        }
    } finally {
        btn.textContent = "开始转换"; btn.disabled = false;
    }
};

function updateUI(count) {
    const urlInput = document.getElementById('m3uUrl');
    urlInput.value = `${window.location.origin}/playlist/tstohls.m3u`;
    urlInput.classList.replace('text-slate-400', 'text-indigo-600');
    document.getElementById('channelCount').textContent = count;
    
    // 更新预览区域的提示文字
    const statusText = document.getElementById('previewStatusText');
    if (count > 0 && statusText) {
        statusText.innerHTML = `<i data-lucide="layout-list" class="w-6 h-6 mb-2 mx-auto opacity-50"></i><p>已加载 ${count} 个频道，请选择频道进行预览</p>`;
        lucide.createIcons(); // 渲染新加入的图标
    }
    
    renderPreview();
}

function renderPreview() {
    if (!channels.length) return;
    const groupsSet = new Set();
    channels.forEach(ch => { if(ch.group) groupsSet.add(ch.group); });
    let keys = Array.from(groupsSet).sort();
    keys.push('全部');
    if (!currentGroup) currentGroup = keys[0];

    const gc = document.getElementById('groupContainer');
    gc.innerHTML = '';
    keys.forEach(g => {
        const btn = document.createElement('button');
        btn.className = `group-tag px-4 py-2 text-xs font-bold whitespace-nowrap ${currentGroup === g ? 'active' : 'bg-white text-slate-400'}`;
        btn.textContent = g;
        btn.onclick = () => { currentGroup = g; renderPreview(); };
        gc.appendChild(btn);
    });

    const grid = document.getElementById('channelGrid');
    grid.innerHTML = '';
    const filtered = currentGroup === '全部' ? channels : channels.filter(c => c.group === currentGroup);
    filtered.forEach(ch => {
        const b = document.createElement('div');
        b.className = 'channel-btn';
        b.innerHTML = `<img src="${ch.logo || ''}" class="w-6 h-6 object-contain" onerror="this.src='/static/logos/favicon.png'"><span class="text-[11px] font-bold truncate">${ch.name}</span>`;
        b.onclick = () => play(ch);
        grid.appendChild(b);
    });
}

function play(ch) {
    // 1. 清空容器（移除“请选择频道”的提示）
    const container = document.getElementById('playerContainer');
    container.innerHTML = ''; 

    if(artPlayer) artPlayer.destroy();
    
    // 2. 初始化播放器
    artPlayer = new Artplayer({
        container: container, 
        url: `/stream/${ch.id}/index.m3u8`, 
        isLive: true, 
        autoplay: true, 
        theme: '#4f46e5',
        fullscreen: true,
        fullscreenWeb: true,
        setting: true,
        // 这里的 customType 保持不变...
        customType: {
            m3u8: (v, u) => {
                if (Hls.isSupported()) { 
                    const hls = new Hls(); hls.loadSource(u); hls.attachMedia(v); 
                } else { v.src = u; }
            }
        }
    });
}

function switchTab(t) {
    const isC = t === 'console';
    document.getElementById('consolePage').classList.toggle('hidden', !isC);
    document.getElementById('previewPage').classList.toggle('hidden', isC);
    document.getElementById('tabConsole').className = isC ? 'font-bold pb-2 border-b-4 border-indigo-600 text-indigo-600' : 'font-bold pb-2 border-b-4 border-transparent text-slate-400';
    document.getElementById('tabPreview').className = !isC ? 'font-bold pb-2 border-b-4 border-indigo-600 text-indigo-600' : 'font-bold pb-2 border-b-4 border-transparent text-slate-400';
}

document.getElementById('tabConsole').onclick = () => switchTab('console');
document.getElementById('tabPreview').onclick = () => switchTab('preview');

document.getElementById('copyBtn').onclick = () => {
    const val = document.getElementById('m3uUrl').value;
    if (!val || val.includes("等待")) return;

    const btn = document.getElementById('copyBtn');
    const success = () => {
        btn.textContent = '已复制';
        setTimeout(() => btn.textContent = '复制', 2000);
    };

    if (navigator.clipboard && window.isSecureContext) {
        navigator.clipboard.writeText(val).then(success);
    } else {
        const textArea = document.createElement("textarea");
        textArea.value = val;
        textArea.style.position = "fixed";
        textArea.style.left = "-9999px";
        textArea.style.top = "0";
        document.body.appendChild(textArea);
        textArea.focus();
        textArea.select();
        try {
            document.execCommand('copy');
            success();
        } catch (err) {
            console.error('Fallback copy failed', err);
        }
        document.body.removeChild(textArea);
    }
};

async function checkStatus() {
    try {
        const r = await fetch('/api/status?t=' + Date.now());
        if (r.ok) {
            const d = await r.json();
            document.getElementById('processCount').textContent = d.active_count;
        }
    } catch(e) {}
}

window.onload = () => { init(); checkStatus(); setInterval(checkStatus, 3000); };