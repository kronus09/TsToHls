## 1. 项目概览

- **项目名称**: TsToHls  
- **语言 / 类型**: Go 后端服务 + 纯静态 Web 前端  
- **核心目的**:  
  - 将 TS 协议的直播流（HTTP/HTTPS/RTP/UDP 等）实时转换为 HLS（`.m3u8` + `.ts`）  
  - 生成一个新的 M3U 播放列表，供 OmniBox 等 IPTV 播放器、浏览器等订阅使用  
  - 提供实验性的 **原流播放**（不走 FFmpeg 转码，仅 PC 端支持）  
- **默认端口**: `15140`  
- **当前版本**: `v1.3.2`  

高层一句话：**给家里的 IPTV 原始 m3u 源套一层网关，统一转成 HLS 或直接透传 TS，并提供 Web 控制台和频道列表管理。**

---

## 2. 目录结构与模块划分（高层）

主要目录（来自 `PROJECT_RECORD.md` 和实际代码）：

- `main.go`  
  - HTTP 服务器入口 + 路由注册  
  - 挂载静态资源、API、HLS 流、代理等  
- `parser/m3u.go`  
  - M3U 解析与生成模块  
  - 校验流是否为 H.264，过滤非法或不兼容源  
  - 生成两个核心文件：
    - `m3u/tstohls.m3u`：对外订阅用的播放列表（指向本服务 `/stream/{id}/index.m3u8`）  
    - `m3u/mapping.json`：频道元数据 + 原始 URL 映射（Web UI/FFmpeg 使用）  
- `manager/ffmpeg.go`  
  - `ProcessManager` 进程管理器  
  - 按频道 ID 启动 / 复用 / 清理 FFmpeg 转码进程  
  - 负责读取 `m3u/mapping.json` 并找到频道对应的原始 URL  
- `m3u/`（运行时数据目录，可挂载到宿主机）  
  - `config.json`：FFmpeg 转码配置（动态可改）  
  - `source.m3u`：最近一次上传/订阅的原始列表（首行带 `TsToHls-Source-URL` 注释）  
  - `tstohls.m3u`：已转换后的 HLS 订阅列表  
  - `mapping.json`：频道列表和元数据（ID、名称、分组、Logo、本地/远程 URL）  
  - `logos/`：下载后的频道图标（供网页展示）  
- `web/`  
  - `index.html`：控制台 + HLS播放 Web UI  
  - `static/`：CSS、JS、播放器库、使用文档 HTML  
- `.github/workflows/`、`Dockerfile`、`docker-compose.yml`：CI/CD 与容器化部署  

---

## 3. 运行时组件与职责

### 3.1 HTTP 服务（`main.go`）

- **静态资源路由**
  - `/static/*` → `web/static`  
  - `/logos/*` → `m3u/logos`（本地下载好的频道图标）  
  - `/m3u/*` → `m3u`（用于原流播放页面直接访问原始 m3u）  
- **前端首页**
  - `/` → `web/index.html`  
- **API 接口**
  - `POST /api/upload`  
    - 入参形式：
      - `multipart/form-data`：上传本地 `m3uFile`，可带 `checkSourceReliability`  
      - `application/json`：`{"url": "...", "checkSourceReliability": true/false}`（远程订阅）  
    - 行为：
      - 写入 `m3u/source.m3u`（远程模式首行写 `# TsToHls-Source-URL: ...`）  
      - 调用 `parser.ParseAndGenerate(...)` 生成 `tstohls.m3u` 和 `mapping.json`  
  - `GET /api/list`  
    - 直接返回 `m3u/mapping.json`，给前端渲染频道列表  
  - `GET /api/status`  
    - 使用 `gopsutil/cpu` + `runtime.MemStats` 获取 CPU/内存  
    - 再加上 `ProcessManager` 的当前进程数与 ID 列表  
  - `GET /api/config` / `POST /api/config` / `POST /api/config?action=reset`  
    - 封装对 `manager.ProcessManager.Config` 的读写，持久化到 `m3u/config.json`  
  - `GET /api/check-source`  
    - 检查 `m3u/source.m3u` 是否存在，并从首行注释解析出 `sourceUrl`  
  - `POST /api/reprocess`  
    - 在不重新下载的情况下，直接用本地 `source.m3u` 再跑一次解析和映射  
- **资源接口**
  - `GET /playlist/tstohls.m3u`  
    - 对外暴露的 HLS 订阅地址（推荐给 IPTV 客户端使用）  
  - `GET /stream/{id}/{file}`  
    - HLS 播放入口：
      - `{file} = index.m3u8` → 触发 `ProcessManager.ensureProcess`，读回 m3u8  
      - `{file} = *.ts` → 直接从 `hls_temp/{id}/` 目录读切片  
  - `GET /proxy/{url...}`  
    - 通用 HTTP 代理：
      - 将 URL 路径中的 `%2F/%3A` 还原为 `/`、`:`  
      - 转发原始请求和大部分头信息  
      - 用于 **原流播放** / 跨域代理原始 TS 源  

---

## 4. M3U 解析与频道映射（`parser/m3u.go`）

### 4.1 核心数据结构

频道数据结构：

```text
Channel {
  ID    string  // ch001, ch002...
  Name  string  // 频道名称
  Logo  string  // Logo URL 或本地图标路径
  Group string  // 分组（上海/央视/卫视/教育/电影等）
  Url   string  // 原始源地址（HTTP/HTTPS/RTP/UDP）
}
```

### 4.2 解析流程（`ParseAndGenerate`）

1. 打开 `inputPath`（如 `m3u/source.m3u`），逐行扫描：  
   - 遇到 `#EXTINF:` 行：解析 `tvg-name`、`tvg-logo`、`group-title`，填入 `Channel` 当前缓存  
   - 遇到 **非注释且是 URL** 行：
     - 确认是 `http/https/rtp/udp` 且非图片扩展名  
     - 写入 `Url` 并分配 ID（`ch001`、`ch002` ...）  
     - 若无 Name 则补成 `未命名-{index}`  
     - 追加进 `rawChannels`  
2. **可选可靠性检测**（`checkReliability == true`）：  
   - 并发限制为最多 5 个 goroutine  
   - 调用 `ValidateStream(url)`：  
     - 用 `ffprobe` 检查 `v:0` 的 `codec_name` 是否包含 `h264` / `avc`  
     - 有 3 次重试，每次 8 秒超时  
   - 只保留 H.264 / AVC 视频流  
3. 结果集排序（按 ID 升序），得到 `validChannels`。  

### 4.3 输出内容

- **订阅 M3U 文件**：`m3u/tstohls.m3u`  
  - 每个频道一段 `#EXTINF` + 本服务 URL：
    - `#EXTINF:-1 tvg-name="..." tvg-logo="原始Logo" group-title="分组",频道名`  
    - `http://{serverAddr}/stream/{ID}/index.m3u8`  
- **本地图标 + 映射 JSON**：  
  - 遍历 `validChannels`，调用 `downloadLogo(id, remoteLogo)`：  
    - 将远程 Logo 下载到 `m3u/logos/{id}.{ext}`  
    - 返回供 Web 使用的 `/logos/{id}.{ext}` 路径  
  - 把更新后 Channel 写入 `m3u/mapping.json`（前端频道列表 + 后端 URL 映射使用）  

---

## 5. 转码进程管理（`manager/ffmpeg.go`）

### 5.1 配置结构 `FFmpegConfig`

可序列化到 `m3u/config.json`，关键字段：

- **MaxProcesses**: 最大并发 FFmpeg 进程数  
- **HlsTime**: 每个切片时长（秒，对应 `-hls_time`）  
- **HlsListSize**: 播放列表中保留的切片数（`-hls_list_size`）  
- **IdleTimeout**: 闲置超时时间（秒），超过后自动停止并清理进程  
- **VideoCodec**: 视频编码器（默认 `copy`，只转音频）  
- **AudioCodec**: 音频编码器（默认 `aac`）  
- **AudioBitrate**: 音频码率（默认 `128k`）  
- **ReconnectDelay**: 断流重连间隔（`-reconnect_delay_max`）  
- **HlsFlags**: HLS 标志（默认 `delete_segments+discont_start+independent_segments`）  
- **HlsSegmentType**: 切片封装类型（默认 `mpegts`）  
- **CheckSourceReliability**: 默认源可靠性检测开关（用于前端默认值）  

### 5.2 ProcessManager 核心职责

- **`ensureProcess(id, outDir)`**
  - 若已存在该 `id` 进程，直接返回  
  - 若进程数已达 `MaxProcesses`，调用 `killOldest()` 杀掉最老、最久未访问的进程  
  - 通过 `getRawUrl(id)` 在 `m3u/mapping.json` 中查出原始 URL  
  - 建立输出目录 `hls_temp/{id}`，构造 FFmpeg 命令：
    - 输入：原始 URL  
    - 输出：`hls_temp/{id}/index.m3u8` + 若干 `.ts`  
  - 启动 FFmpeg 子进程，并记录到 `Processes` map  
  - 监听 `cmd.Wait()`，结束后从 map 中清理  
- **`GetM3u8Content(id, baseDir)`**
  - 调用 `ensureProcess` 确保进程存在  
  - 轮询 `index.m3u8` 是否生成（最多 60 次 * 0.5s）  
  - 返回 m3u8 内容给 HTTP handler  
- **闲置清理（`cleanupLoop`）**
  - 每 30 秒检测一次所有进程：  
  - 若 `now - LastAccess` > `IdleTimeout`，则 Kill 进程 + 删除对应临时目录  

---

## 6. Web 前端与交互（`web/index.html` + `static/js/app.js`）

高层行为：

- **控制台页（默认 Tab）**
  - 显示：
    - 活跃进程数、频道总数、CPU 占用、内存占用（轮询 `/api/status` + `/api/list`）  
  - 数据导入：
    - 拖拽 / 点选上传本地 `.m3u` 文件（`/api/upload`）  
    - 填写远程订阅地址（`/api/upload` JSON 模式）  
    - `仅保留 H.264` 开关 → 控制 `checkSourceReliability` 参数  
  - 订阅地址区域：
    - 文本框自动填充 `http://{host}/playlist/tstohls.m3u`  
    - 一键复制按钮  
  - **原流播放**：
    - 跳转 `/static/docs/direct-ts.html`
    - 利用 `/m3u/source.m3u` + `/proxy/` 实现直接 TS 播放
    - 仅 PC 端浏览器支持，移动端请使用 HLS播放  
  - FFMPEG 专家配置：
    - 表单从 `/api/config` 加载参数  
    - “编辑配置”按钮 → 解锁下拉框 → 提交到 `POST /api/config` 保存  

- **HLS播放页**
  - 使用 ArtPlayer + HLS.js  
  - 渲染频道分组（Group）和频道按钮列表  
  - 点击频道：
    - 用 `/stream/{id}/index.m3u8` 作为播放 URL  
    - 触发后端 FFmpeg 启动或 KeepAlive  

---

## 7. 端到端核心业务流程

### 7.1 从原始订阅到 HLS 订阅

1. 用户通过 Web 控制台：  
   - 上传 `m3u` 文件 或 提交一个远程订阅 URL  
2. 后端 `POST /api/upload`：  
   - 将数据写入 `m3u/source.m3u`（远程模式首行写 `# TsToHls-Source-URL: ...`）  
   - 调用 `parser.ParseAndGenerate(source.m3u, http://{host}, checkReliability)`  
3. `ParseAndGenerate`：  
   - 解析 M3U → 过滤非法/非视频 URL →（可选）用 ffprobe 验证 H.264  
   - 生成：
     - `m3u/tstohls.m3u`（频道指向 `http://{host}/stream/{id}/index.m3u8`）  
     - `m3u/mapping.json`（包含 ID、Name、Group、Logo、本地化 Logo 路径和源 URL）  
4. 用户把播放器指向：  
   - `http://{host}:15140/playlist/tstohls.m3u` 进行订阅  

### 7.2 播放某个频道（HLS 模式）

1. 播放器解析 `tstohls.m3u`，请求：  
   - `GET /stream/ch001/index.m3u8`  
2. `streamHandler`：  
   - `pm.KeepAlive("ch001")`  
   - 调用 `pm.GetM3u8Content("ch001", "hls_temp")`：  
     - 若无进程 → `ensureProcess` → 启动 FFmpeg 转码  
     - 等待最多 30s 得到 `index.m3u8` 内容  
3. 播放器再请求 `.ts` 切片：  
   - `GET /stream/ch001/xxx.ts` → 直接从 `hls_temp/ch001/xxx.ts` 读文件返回  
4. `ProcessManager.cleanupLoop` 负责在长时间无访问时停止该进程并清理磁盘。  

### 7.3 原流播放（非转码，仅PC端）

1. 前端 `direct-ts.html` 直接读 `m3u/source.m3u` 或使用 `/proxy/`  
2. 使用 mpegts.js 在浏览器中直接播放原始 TS 流  
3. 用于：
   - 检查源是否可用  
   - 对比转码前后的延迟和质量  
4. 兼容性：仅 PC 端浏览器支持，iPhone、iPad 和安卓设备不支持  

---

## 8. AI 二次开发 / 接入建议

如果后续需要让其他 AI 或自动化工具基于 TsToHls 做集成或扩展，可以优先关注：

- **接口层（推荐作为 AI 交互入口）**  
  - `POST /api/upload`：自动更新频道源（定时任务 / 订阅刷新）  
  - `POST /api/reprocess`：不重新下载，重跑解析逻辑（比如过滤策略变化）  
  - `GET /api/list`：获取当前可用频道列表（可用于推荐系统 / 自动分组）  
  - `GET /api/status`：获取系统负载和转码进程信息（用于自动扩缩容或报警）  
  - `GET /api/config` + `POST /api/config`：动态调节 FFmpeg 相关性能参数  

- **内部模块扩展点**  
  - `parser.Channel` 增加更多字段（如自定义排序权重、地区、标签等）  
  - `ValidateStream` 中增添码率、分辨率等检测逻辑，用于更智能的源过滤  
  - `ProcessManager` 引入优先级调度（`mapping.json` 中加字段 + 启动时选择性 kill/保留）  

