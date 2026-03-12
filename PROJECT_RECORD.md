# TsToHls 项目记录文件

## 项目基本信息

- **项目名称**: TsToHls
- **项目类型**: Go 语言后端服务
- **主要功能**: 将 TS 协议直播流转换为 HLS 格式，支持直接播放 TS 流
- **版本**: v1.3.1
- **运行端口**: 15140

## 项目结构

```
tstohls/
├── .github/             # GitHub 工作流配置
│   └── workflows/       # CI/CD 配置
├── manager/             # 进程管理模块
│   └── ffmpeg.go        # FFmpeg 进程管理
├── parser/              # M3U 解析模块
│   └── m3u.go           # M3U 文件解析和生成
├── web/                 # 前端界面
│   ├── static/          # 静态资源
│   │   ├── css/         # CSS 文件
│   │   ├── docs/        # 文档
│   │   ├── js/          # JavaScript 文件
│   │   └── logos/       # 频道图标
│   ├── index.html       # 主页面
│   └── input.css        # Tailwind 输入文件
├── .gitignore           # Git 忽略文件
├── Dockerfile           # Docker 构建文件
├── README.md            # 项目说明文档
├── docker-compose.yml   # Docker Compose 配置
├── go.mod               # Go 模块依赖
├── go.sum               # Go 模块校验和
└── main.go              # 主入口文件
```

## 核心功能模块

### 1. 直播流转码
- **功能**: 将 TS 协议直播流转换为 HLS 格式
- **实现**: 使用 FFmpeg 进行转码，仅转换音频为 AAC 格式，保留 H.264 视频
- **特点**: 轻量高效，系统负载低

### 2. Direct TS 播放
- **功能**: 直接在浏览器中播放原始 TS 流
- **优势**: 更低延迟，更快启动，无需转码
- **使用场景**: 验证原始源是否可用，低延迟观看

### 3. M3U 文件管理
- **功能**: 支持上传本地 M3U 文件和远程订阅地址导入
- **特性**: 支持仅保留 H.264 格式的视频流
- **输出**: 生成标准化的 M3U 频道订阅文件

### 4. Web 管理界面
- **功能**: 提供直观的 Web 界面管理频道和配置
- **特性**: 分组频道显示，频道数量统计，实时播放预览
- **技术**: 使用 Tailwind CSS 构建响应式界面

### 5. 系统监控
- **功能**: 实时监控系统状态和转码进程
- **指标**: CPU 使用率，内存使用，活跃转码进程数
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
  - **返回**: `{"active_count": 活跃进程数, "running_ids": [进程ID], "cpu": "CPU使用率", "mem": "内存使用"}`

- **`GET /api/list`**: 获取频道列表
  - **返回**: 频道映射 JSON 数据

- **`GET /api/config`**: 获取 FFmpeg 配置
  - **返回**: FFmpeg 配置 JSON 数据

- **`POST /api/config`**: 更新 FFmpeg 配置
  - **参数**: FFmpeg 配置 JSON 数据
  - **返回**: `{"status": "ok", "message": "配置保存成功"}`

- **`POST /api/config?action=reset`**: 重置配置为默认值
  - **返回**: `{"status": "ok", "message": "已恢复默认配置"}`

- **`GET /api/check-source`**: 检查 source.m3u 文件是否存在并提取订阅地址
  - **返回**: `{"exists": true/false, "sourceUrl": "订阅地址"}`

- **`POST /api/reprocess`**: 重新处理 source.m3u 文件
  - **参数**: `{"checkSourceReliability": true}`
  - **返回**: `{"status": "ok", "count": 频道数, "message": "解析完成"}`

### 3. 资源接口
- **`GET /playlist/tstohls.m3u`**: 获取生成的 M3U 播放列表
- **`GET /stream/{id}/{file}`**: 获取转码后的 HLS 流
- **`GET /proxy/{url}`**: 代理访问原始 TS 流

## 技术栈

### 后端
- **语言**: Go 1.20+
- **Web 框架**: 标准库 `net/http`
- **依赖**: 
  - `github.com/shirou/gopsutil/v3` (系统监控)
  - FFmpeg (转码工具)

### 前端
- **HTML5**: 页面结构
- **CSS**: Tailwind CSS (响应式设计)
- **JavaScript**: 前端交互
- **播放器**: ArtPlayer (视频播放)

### 部署
- **容器化**: Docker + Docker Compose
- **持久化**: 挂载 `./m3u` 目录存储 M3U 文件
- **性能优化**: 使用 tmpfs 挂载 `hls_temp` 目录到内存

## 配置选项

### FFmpeg 配置
- **最大并发进程数**: 限制同时转码的频道数
- **切片时长**: HLS 切片的时长（秒）
- **列表容量**: HLS 播放列表的容量
- **闲置超时**: 闲置频道的超时时间（秒）
- **视频编码器**: 视频编码格式
- **音频编码器**: 音频编码格式

### 环境变量
- **GIN_MODE**: 运行模式（release 或 debug）
- **TZ**: 时区设置（默认 Asia/Shanghai）

## 部署方法

### Docker Compose 部署
1. 创建 `docker-compose.yml` 文件
2. 启动服务: `docker-compose up -d`
3. 访问管理界面: `http://服务器IP:15140`
4. 订阅地址: `http://服务器IP:15140/playlist/tstohls.m3u`

### 本地开发
1. 安装 Go 1.20+ 和 FFmpeg
2. 克隆项目: `git clone https://github.com/kronus09/tstohls.git`
3. 安装依赖: `go mod download`
4. 运行服务: `go run main.go`

## 核心流程

1. **上传 M3U 文件**或**导入订阅地址**
2. **解析 M3U 文件**，提取频道信息
3. **生成映射文件**，创建频道 ID 和原始 URL 的映射
4. **生成播放列表**，替换原始 URL 为本地转码 URL
5. **用户访问频道**时，启动 FFmpeg 进程进行转码
6. **提供 HLS 流**给用户播放器
7. **监控系统状态**，管理转码进程

## 关键模块解析

### main.go
- **主入口文件**，负责初始化服务和设置路由
- **核心函数**:
  - `main()`: 初始化进程管理器，设置路由，启动 HTTP 服务
  - `uploadHandler()`: 处理 M3U 文件上传和订阅地址导入
  - `streamHandler()`: 处理 HLS 流请求，启动转码进程
  - `proxyHandler()`: 代理访问原始 TS 流
  - `statusHandler()`: 提供系统状态监控数据

### manager/ffmpeg.go
- **进程管理模块**，负责管理 FFmpeg 转码进程
- **核心功能**:
  - 启动和停止转码进程
  - 生成 HLS 播放列表
  - 管理进程生命周期
  - 配置管理和持久化

### parser/m3u.go
- **M3U 解析模块**，负责解析和生成 M3U 文件
- **核心功能**:
  - 解析 M3U 文件，提取频道信息
  - 检查源可靠性
  - 生成标准化的 M3U 播放列表
  - 处理频道分组和图标

## 性能优化

1. **内存盘存储**: 使用 tmpfs 挂载 `hls_temp` 目录到内存，提高 I/O 性能
2. **仅转音频**: 保留 H.264 视频，只转换音频为 AAC，减少 CPU 负载
3. **进程管理**: 智能管理转码进程，闲置超时自动停止
4. **并发控制**: 通过配置限制最大并发进程数，避免系统过载

## 常见问题

### 转码失败
- **原因**: M3U 文件格式错误，订阅地址不可访问，或源不可用
- **解决**: 检查 M3U 文件格式，验证订阅地址是否可访问，或尝试使用 Direct TS 播放验证源

### 播放卡顿
- **原因**: 网络带宽不足，服务器资源不足，或转码配置不当
- **解决**: 增加服务器带宽和资源，调整 FFmpeg 配置中的切片时长和列表容量

### 更新源
- **方法**: 如果是订阅地址导入的，直接点击「上传并转换」按钮重新拉取；如果是本地文件导入的，重新上传更新后的 M3U 文件

## 支持的播放器

- **桌面端**: PotPlayer, VLC, IINA, MPV
- **移动端**: nPlayer, Fileball, APM, Kodi
- **浏览器**: 支持 HLS 的现代浏览器（Chrome, Firefox, Safari）

## 版权信息

本项目采用 MIT License 协议。

Copyright (c) 2026 kronus09.
