# TsToHls - 直播流转码工具

将 TS 协议直播流转换为 HLS 格式的工具，支持 M3U 频道列表管理。

## 特性

- 📺 实时转码 TS 协议为 HLS
- 🗂 M3U/M3U8 文件管理
- 🚀 轻量高效，支持容器化部署
- 🎨 简易Web管理界面

## 推荐安装方式

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
http://服务器IP:15140/web
```

## 配置说明

- **数据持久化**：`./data/m3u` 目录存储上传的 M3U 文件
- **临时存储**：HLS 切片使用内存盘提高性能
- **时区**：默认 `Asia/Shanghai`，可按需修改

## 版权信息

开源协议
本项目采用 MIT License 协议。

Copyright (c) 2026 kronus09.
