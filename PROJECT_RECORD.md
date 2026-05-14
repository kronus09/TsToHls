# TsToHls 项目记录文件

## 项目基本信息

- **项目名称**: TsToHls
- **项目类型**: Go 语言后端服务
- **主要功能**: 将 TS 协议直播流转换为 HLS 格式，支持直接播放 TS 流
- **版本**: v1.4.0
- **运行端口**: 15140

## 项目结构

```
TsToHls/
├── main.go                      # 入口（路由注册 + 服务启动）
├── go.mod / go.sum              # Go 模块依赖
├── Dockerfile                   # 多阶段 Docker 构建（含 FFmpeg n8.0 源码编译）
├── docker-compose.yml           # Docker Compose 部署配置
│
├── internal/                    # 后端 Go 包（internal 防止外部引用）
│   ├── handler/                 #   HTTP handler
│   │   └── handler.go
│   ├── manager/                 #   FFmpeg 进程管理
│   │   └── ffmpeg.go
│   ├── parser/                  #   M3U 解析与频道映射生成
│   │   └── m3u.go
│   ├── probe/                   #   go-astiav 进程内源分析（替代 ffprobe CLI）
│   │   └── probe.go
│   └── db/                      #   SQLite 数据库操作
│       └── db.go
│
├── web/                         # 前端静态资源
│   ├── index.html
│   ├── input.css
│   ├── logo.png
│   └── static/
│       ├── style.css
│       ├── css/
│       ├── js/
│       ├── docs/
│       └── logos/               # 默认频道图标
│
├── data/                        # 持久化目录（Docker 挂载）
│   ├── tstohls.db               #   SQLite 数据库（WAL 模式）
│   ├── config.json              #   FFmpeg 运行配置
│   ├── source.m3u               #   原始 M3U 源文件
│   ├── tstohls.m3u              #   生成的 HLS 订阅列表
│   ├── mapping.json             #   频道映射（兼容保留）
│   └── logos/                   #   本地化频道图标
│
└── hls_temp/                    # 运行时临时目录（tmpfs/内存盘）
    └── {channelID}/             #   HLS 切片
```

## 核心功能模块

### 1. 直播流转码
- **功能**: 将 TS 协议直播流转换为 HLS 格式
- **实现**: 使用 FFmpeg 进行转码，仅转换音频为 AAC 格式，保留 H.264 视频
- **特点**: 转量高效，系统负载低

### 2. 源分析（go-astiav 进程内探测）
- **功能**: 导入直播源时分析视频格式，元数据写入 SQLite
- **实现**: 使用 go-astiav（FFmpeg n8.0 CGO 绑定）替代 ffprobe CLI
- **优势**: 零进程启动开销，分层递进探测（32K→256K→1M→5M），IOInterrupter 超时保护
- **缓存**: 探测结果存 SQLite，播放时查表跳过探测

### 3. 原流播放
- **功能**: 直接在浏览器中播放原始 TS 流
- **优势**: 更低延迟，更快启动，无需转码
- **兼容性**: 仅 PC 端浏览器支持

### 4. M3U 文件管理
- **功能**: 支持上传本地 M3U 文件和远程订阅地址导入
- **特性**: 支持仅保留 H.264 格式的视频流，频道启用/禁用开关
- **输出**: 生成标准化的 M3U 频道订阅文件

### 5. Web 管理界面
- **功能**: 提供直观的 Web 界面管理频道和配置
- **技术**: Tailwind CSS + 原生 JS + ArtPlayer

### 6. 系统监控
- **功能**: 实时监控系统状态和转码进程
- **指标**: CPU 使用率，内存使用，活跃转码进程数，频道总数，启用频道数
- **API**: `/api/status` 接口提供监控数据

## API 接口

### 1. 上传和解析接口
- **`POST /api/upload`**: 上传 M3U 文件或订阅地址
  - **参数**: 
    - `m3uFile`: 本地 M3U 文件 (multipart/form-data)
    - 或 `{"url": "订阅地址", "checkSourceReliability": true}` (application/json)
  - **返回**: `{"status": "ok", "count": 频道数, "message": "解析完成"}`

### 2. 状态和管理接口
- **`GET /api/status`**: 获取系统状态
  - **返回**: `{"active_count", "running_ids", "cpu", "mem", "total_count", "enabled_count"}`

- **`GET /api/list`**: 获取频道列表（从 SQLite 读取）
  - **返回**: 频道 JSON 数据（含 enabled 字段）

- **`GET /api/config`**: 获取 FFmpeg 配置
- **`POST /api/config`**: 更新 FFmpeg 配置
- **`POST /api/config?action=reset`**: 重置配置为默认值

- **`GET /api/check-source`**: 检查源文件是否存在并提取订阅地址
- **`POST /api/reprocess`**: 重新处理源文件

### 3. 频道控制接口
- **`POST /api/channel/toggle`**: 切换频道启用/禁用
  - **参数**: `{"id": "ch001"}`
  - **返回**: `{"status": "ok", "enabled": true, "id": "ch001"}`

- **`POST /api/channel/set-enabled`**: 设置频道启用状态
  - **参数**: `{"id": "ch001", "enabled": true}`
  - **返回**: `{"status": "ok", "enabled": true, "id": "ch001"}`

### 4. 资源接口
- **`GET /playlist/tstohls.m3u`**: 获取生成的 M3U 播放列表
- **`GET /stream/{id}/{file}`**: 获取转码后的 HLS 流
- **`GET /proxy/{url}`**: 代理访问原始 TS 流

## 技术栈

### 后端
- **语言**: Go 1.22+
- **Web 框架**: 标准库 `net/http`
- **依赖**: 
  - `github.com/asticode/go-astiav` v0.40.0 — FFmpeg n8.0 CGO 绑定（源分析）
  - `github.com/mattn/go-sqlite3` — SQLite CGO 驱动
  - `github.com/shirou/gopsutil/v3` — 系统监控
  - `github.com/fsnotify/fsnotify` — 文件监听
- **FFmpeg**: n8.0（Docker 内源码编译，含 libx264）

### 前端
- **HTML5**: 页面结构
- **CSS**: Tailwind CSS v4.2.1
- **JavaScript**: 原生 JS
- **播放器**: ArtPlayer + HLS.js + mpegts.js

### 部署
- **容器化**: Docker 多阶段构建 + Docker Compose
- **持久化**: 挂载 `./data` 目录（SQLite + 配置 + 图标 + M3U）
- **性能优化**: `/dev/shm` tmpfs 内存盘存储 HLS 切片（零磁盘 IO）

## 数据库设计

### channels 表
| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT PK | 频道 ID（ch001~chNNN） |
| name | TEXT | 频道名称 |
| logo | TEXT | Logo URL |
| grp | TEXT | 分组 |
| url | TEXT | 源流 URL |
| video_codec | TEXT | 视频编码 |
| audio_codec | TEXT | 音频编码 |
| width/height | INTEGER | 分辨率 |
| frame_rate | TEXT | 帧率 |
| audio_sample | INTEGER | 采样率 |
| input_format | TEXT | 输入格式 |
| enabled | INTEGER | 启用状态（0/1） |
| created_at/updated_at | DATETIME | 时间戳 |

### source 表
| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER PK | 固定为 1 |
| url | TEXT | 订阅 URL |
| file_path | TEXT | 源文件路径 |

## 配置选项

### FFmpeg 配置（data/config.json）
- **max_processes**: 最大并发进程数（默认 6）
- **hls_time**: 切片时长（默认 1 秒）
- **hls_list_size**: 播放列表容量（默认 3）
- **idle_timeout**: 闲置超时（默认 120 秒）
- **hls_temp_dir**: HLS 临时目录（默认 /dev/shm/tstohls）
- **low_latency_mode**: 低延迟模式（默认 true）
- **audio_codec**: 音频编码器（默认 aac）
- **check_source_reliability**: 源可靠性检查开关（默认 true）

## 核心流程

1. **上传 M3U 文件**或**导入订阅地址**
2. **解析 M3U 文件**，提取频道信息
3. **go-astiav 进程内探测**，分析视频/音频编码、分辨率、帧率
4. **元数据写入 SQLite**，重复导入时保留 enabled 状态
5. **生成播放列表**，替换原始 URL 为本地转码 URL
6. **用户访问频道**时，启动 FFmpeg 进程进行转码
7. **提供 HLS 流**给用户播放器（从 /dev/shm 读取）
8. **监控系统状态**，管理转码进程

## v1.4.0 改造记录

### 阶段一：数据层改造
- 引入 SQLite（go-sqlite3 + WAL 模式）替代 mapping.json
- 新增 `enabled` 字段，支持频道启用/禁用
- 新增 `/api/channel/toggle` 和 `/api/channel/set-enabled` API
- HLS 输出路径默认改为 `/dev/shm/tstohls`（零磁盘 IO）
- 持久化目录统一为 `data/`

### 阶段二：go-astiav 核心集成（源分析模块）
- 引入 go-astiav v0.40.0（FFmpeg n8.0 CGO 绑定）
- 创建 `internal/probe` 包，替代 ffprobe CLI 调用
- 分层递进探测（32K→256K→1M→5M），IOInterrupter 超时保护
- 探测结果写入 SQLite，播放时查表跳过探测（消除重复探测开销）
- Dockerfile 改为源码编译 FFmpeg n8.0

### 目录重组
- Go 包移入 `internal/`（handler/manager/parser/probe/db）
- handler 从 main.go 拆分到 `internal/handler/`
- 数据文件从 `m3u/` 迁移到 `data/`（config.json/source.m3u/tstohls.m3u/logos/）
- 删除 `m3u/` 目录，消除历史命名混淆
- Docker 挂载从 `./m3u:/app/m3u` 改为 `./data:/app/data`

## 部署方法

### Docker Compose 部署
1. 创建 `docker-compose.yml` 文件
2. 启动服务: `docker-compose up -d`
3. 访问管理界面: `http://服务器IP:15140`
4. 订阅地址: `http://服务器IP:15140/playlist/tstohls.m3u`

### 本地开发
1. 安装 Go 1.22+ 和 FFmpeg n8.0 dev 库
2. 设置环境变量: `PKG_CONFIG_PATH`, `CGO_CFLAGS`, `CGO_LDFLAGS`
3. 运行服务: `CGO_ENABLED=1 go run main.go`

## 性能优化

1. **内存盘存储**: HLS 切片输出到 `/dev/shm/tstohls`（零磁盘 IO）
2. **仅转音频**: 保留 H.264 视频，只转换音频为 AAC，减少 CPU 负载
3. **进程内探测**: go-astiav 替代 ffprobe CLI，零进程启动开销
4. **元数据缓存**: 探测结果存 SQLite，播放时查表跳过探测
5. **进程管理**: 智能管理转码进程，闲置超时自动停止
6. **并发控制**: 通过配置限制最大并发进程数，避免系统过载

## 后续规划

- **阶段三**: 常驻切片器（goroutine 常驻 + skip_frame 关键帧对齐 + io.Pipe 管道）
- **阶段四**: 无缝换源（discontinuity 标记 + 多源管理 + 健康检测）
- **阶段五**: SegmentStore 内存直出（mpegts muxer + 自定义 IO + 零磁盘）

## 版权信息

本项目采用 MIT License 协议。

Copyright (c) 2026 kronus09.
