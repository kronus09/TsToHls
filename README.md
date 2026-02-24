# TsToHls - 视频流转换工具

轻量级 M3U 视频流转换为 HLS (m3u8) 代理流服务。

## 功能特性

- M3U 播放列表解析与转换
- FFmpeg 实时转码（视频 copy，音频转 aac）
- 进程管理与自动清理（LRU 算法，最多 5 个并发进程）
- 3 分钟无活动自动清理
- 前端界面支持频道分组与播放

## 快速开始

### 本地运行

```bash
go run main.go
```

访问 http://localhost:15140

### Docker 部署

```bash
# 构建并启动
docker-compose up --build -d

# 查看日志
docker-compose logs -f

# 停止服务
docker-compose down
```

访问 http://localhost:15140

## 技术栈

- 后端：Go + Gin
- 前端：HTML + Tailwind CSS + ArtPlayer
- 转码：FFmpeg

## 目录结构

```
TsToHls/
├── main.go           # 主程序入口
├── manager/          # FFmpeg 进程管理
├── parser/          # M3U 解析器
├── web/             # 前端页面
├── data/            # 数据目录
│   ├── m3u/        # 上传的 M3U 文件
│   └── hls/        # HLS 切片文件 (tmpfs)
├── Dockerfile       # 容器构建
└── docker-compose.yml # 容器编排
```

## API 接口

- `POST /api/upload` - 上传 M3U 文件
- `GET /api/status` - 获取服务状态
- `GET /stream/:id/index.m3u8` - 获取 HLS 流

## 配置

- 服务端口：15140
- 最大并发进程：5
- 进程超时：3 分钟
- HLS 切片时长：4 秒
- HLS 列表大小：5 个切片