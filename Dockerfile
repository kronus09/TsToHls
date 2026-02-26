# ===========================================
# 第一阶段：构建 Go 程序
# ===========================================
FROM golang:1.23-alpine AS builder

# 设置 Go 环境
ENV CGO_ENABLED=0 GOOS=linux
WORKDIR /app

# 优化点：使用通配符拷贝 go.mod 和 go.sum（如果存在）
# 这样即使没有第三方依赖导致缺少 go.sum，构建也不会失败
COPY go.mod go.sum* ./
# 只有当 go.mod 包含依赖时才运行下载
RUN if [ -f go.sum ]; then go mod download; fi

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

# --- 创建符合最新逻辑的目录 ---
# 1. m3u/logos: 存放频道图标
# 2. hls_temp: 存放 FFmpeg 实时生成的切片文件
RUN mkdir -p ./m3u/logos ./hls_temp && chmod -R 777 ./m3u ./hls_temp

# 暴露端口
EXPOSE 15140

# 启动命令
CMD ["./tstohls"]