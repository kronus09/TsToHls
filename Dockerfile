# ===========================================
# 第一阶段：构建 Go 程序
# ===========================================
FROM golang:1.23-alpine AS builder

# 设置工作目录
WORKDIR /app

# 拷贝源代码
COPY . .

# 下载依赖并整理
RUN go mod tidy

# 编译生成二进制文件
RUN go build -o tstohls .

# ===========================================
# 第二阶段：运行镜像
# ===========================================
FROM alpine:latest

# 安装 FFmpeg
RUN apk add --no-cache ffmpeg

# 设置工作目录
WORKDIR /app

# 拷贝构建好的二进制文件
COPY --from=builder /app/tstohls .

# 拷贝 web 静态目录
COPY --from=builder /app/web ./web

# 拷贝 data 目录结构
COPY --from=builder /app/data ./data

# 清理缓存目录
RUN rm -rf /app/data/hls/* /app/data/m3u/*

# 暴露端口
EXPOSE 15140

# 启动命令
CMD ["./tstohls"]