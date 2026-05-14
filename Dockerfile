# ===========================================
# 第一阶段：构建 FFmpeg
# ===========================================
FROM golang:1.22-alpine AS ffmpeg-builder

RUN apk add --no-cache nasm yasm git make gcc musl-dev \
    pkgconfig coreutils

RUN git clone --depth 1 --branch n8.0 https://github.com/FFmpeg/FFmpeg.git /tmp/ffmpeg && \
    cd /tmp/ffmpeg && \
    ./configure --prefix=/opt/ffmpeg --enable-gpl \
      --enable-shared --disable-static --disable-doc --disable-ffplay \
      --disable-ffmpeg --disable-ffprobe && \
    make -j$(nproc) && make install && \
    rm -rf /tmp/ffmpeg

# ===========================================
# 第二阶段：构建 Go 程序
# ===========================================
FROM golang:1.22-alpine AS go-builder

RUN apk add --no-cache gcc musl-dev pkgconfig

COPY --from=ffmpeg-builder /opt/ffmpeg /opt/ffmpeg

ENV CGO_ENABLED=1
ENV PKG_CONFIG_PATH=/opt/ffmpeg/lib/pkgconfig
ENV CGO_CFLAGS="-I/opt/ffmpeg/include"
ENV CGO_LDFLAGS="-L/opt/ffmpeg/lib -Wl,-rpath,/opt/ffmpeg/lib"

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o tstohls .

# ===========================================
# 第三阶段：运行镜像
# ===========================================
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

ENV TZ=Asia/Shanghai

WORKDIR /app

COPY --from=ffmpeg-builder /opt/ffmpeg/lib /opt/ffmpeg/lib
COPY --from=go-builder /app/tstohls .
COPY --from=go-builder /app/web ./web

RUN arch=$(uname -m) && \
    if [ "$arch" = "x86_64" ]; then ld_path="/etc/ld-musl-x86_64.path"; \
    elif [ "$arch" = "aarch64" ]; then ld_path="/etc/ld-musl-aarch64.path"; fi && \
    echo "/opt/ffmpeg/lib" > "$ld_path"

RUN mkdir -p ./data/logos && chmod -R 777 ./data

EXPOSE 15140

CMD ["./tstohls"]
