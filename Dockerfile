# ===========================================
# 第一阶段：构建 Go 程序
# ===========================================
FROM golang:1.23-alpine AS builder

# 设置 Go 环境
ENV CGO_ENABLED=0 GOOS=linux
WORKDIR /app

# 利用 Docker 缓存层加速依赖下载
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o tstohls .

# ===========================================
# 第二阶段：运行镜像
# ===========================================
FROM alpine:latest

# 安装 FFmpeg (含ffprobe) 和 基础证书
RUN apk add --no-cache ffmpeg ca-certificates tzdata

# 设置时区为上海
ENV TZ=Asia/Shanghai

WORKDIR /app

# 拷贝构建好的二进制文件
COPY --from=builder /app/tstohls .
# 拷贝静态资源
COPY --from=builder /app/web ./web

# --- 核心修改：创建符合最新逻辑的目录 ---
# 1. m3u: 存放 mapping.json 和订阅文件
# 2. hls_temp: 存放 FFmpeg 实时生成的切片文件
RUN mkdir -p ./m3u ./hls_temp

# 暴露端口
EXPOSE 15140

# 启动命令
CMD ["./tstohls"]