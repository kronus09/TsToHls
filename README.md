# TsToHls - 直播流转码工具

将 TS 协议直播流转换为 HLS 格式的工具，让家里的 IPTV 可在浏览器内播放，支持输出 M3U 频道订阅，使用 OmniBox 等项目播放更顺畅。

## 特性

- 📺 实时转码 TS 协议为 HLS（配合组播转单播软使用，仅保留 h264 的 ts 流切片为 hls 流，音频转码为 acc）
- 🔄 **Direct TS 实验播放**：直接在浏览器中播放原始 TS 流，无需转码，更低延迟，更快启动
- 🗂 M3U/M3U8 文件管理
- 🚀 轻量高效（只转音频，系统负载低），支持容器化部署
- 🎨 简易 Web 管理界面，可用页面直接播放预览
- 📊 分组频道显示，支持频道数量统计
- 🔧 FFMPEG 专家配置，可调整各种转码参数

## 快速开始

### Docker Compose 部署

1. 创建 `docker-compose.yml` 文件：

```yaml
services:
  tstohls:
    image: ghcr.io/kronus09/tstohls:latest
    container_name: tstohls
    restart: unless-stopped
    ports:
      - "15140:15140"
    volumes:
      - ./m3u:/app/m3u
      # 如果你需要手动上传原始 iptv.m3u 到容器，也可以映射整个根目录或特定文件
      # - ./iptv.m3u:/app/iptv.m3u
    tmpfs:
      # 将切片目录 hls_temp 挂载到内存中
      # size=512M 足以支撑 10-20 个频道同时点播（每个频道切片约占用 20-30MB）
      - /app/hls_temp:size=512M,mode=1777,exec
    environment:
      - GIN_MODE=release
      - TZ=Asia/Shanghai
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
```

2. 启动服务：
```bash
docker-compose up -d
```

3. 访问管理界面：
```
http://服务器IP:15140
```

## 功能说明

### 数据导入
- 支持本地 M3U 文件上传
- 支持远程订阅地址导入
- 提供「仅保留 H.264 格式」选项，过滤非 H.264 编码的视频流

### 直播预览
- 实时播放已转换的 HLS 流
- 按分组显示频道列表
- 分组按钮显示频道数量统计

### Direct TS 实验播放
- **更低延迟**：直接播放原始 TS 流，省去转码时间
- **更快启动**：无需等待 FFmpeg 初始化
- **源对照**：可用于验证原始源是否可用
- **使用方式**：在「订阅地址」板块底部点击「Direct TS 实验播放」进入

### 专家配置
- 最大并发进程数
- 切片时长
- 列表容量
- 闲置超时
- 视频/音频编码器

## 配置说明

- **数据持久化**：`./m3u` 目录存储上传的 M3U 文件
- **临时存储**：HLS 切片使用内存盘提高性能
- **时区**：默认 `Asia/Shanghai`，可按需修改

## 系统要求

- Docker 环境
- 推荐 2GB 以上内存（支持更多并发频道）
- 稳定的网络连接

## 常见问题

**Q: 转换失败怎么办？**
A: 请检查 M3U 文件格式是否正确，或订阅地址是否可访问。如果问题持续存在，请查看终端中的错误信息。

**Q: 播放时出现卡顿怎么办？**
A: 可以尝试调整 FFMPEG 配置中的切片时长和列表容量，或增加服务器的网络带宽和硬件资源。

**Q: 如何更新源？**
A: 如果是通过订阅地址导入的，可以直接点击「上传并转换」按钮，系统会从订阅地址重新拉取最新的源。如果是通过本地文件导入的，可以重新上传更新后的 M3U 文件。

**Q: 支持哪些播放器？**
A: 支持所有支持 HLS 格式的播放器，包括但不限于：
- 桌面端：PotPlayer、VLC、IINA、MPV
- 移动端：nPlayer、Fileball、APM、Kodi
- 浏览器：支持 HLS 的现代浏览器（如 Chrome、Firefox、Safari）

## 版权信息

本项目采用 MIT License 协议。

Copyright (c) 2026 kronus09.
