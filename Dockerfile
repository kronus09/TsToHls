# ===========================================
# 第一阶段：构建 Go 程序
# ===========================================
FROM golang:1.23-alpine AS builder

# 设置 Go 环境（确保跨平台编译稳定）
ENV CGO_ENABLED=0 GOOS=linux
WORKDIR /app

# 优化点：先拷贝 mod 文件，利用 Docker 缓存层加速依赖下载
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o tstohls .

# ===========================================
# 第二阶段：运行镜像
# ===========================================
FROM alpine:latest

# 安装 FFmpeg 和 基础证书（用于访问 HTTPS 链接）
RUN apk add --no-cache ffmpeg ca-certificates tzdata

# 设置时区为上海（防止日志时间对不上）
ENV TZ=Asia/Shanghai

WORKDIR /app

# 拷贝构建好的二进制文件
COPY --from=builder /app/tstohls .
# 拷贝静态资源
COPY --from=builder /app/web ./web

# 优化点：创建必要的持久化目录，确保即使没有数据也能启动
RUN mkdir -p ./data/hls ./data/m3u

# 暴露端口
EXPOSE 15140

# 启动命令
CMD ["./tstohls"]