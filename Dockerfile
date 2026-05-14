# ===========================================
# 第一阶段：构建 Go 程序
# ===========================================
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache gcc musl-dev nasm yasm git make

ENV CGO_ENABLED=1
WORKDIR /app

RUN git clone --depth 1 --branch n8.0 https://github.com/FFmpeg/FFmpeg.git /tmp/ffmpeg && \
    cd /tmp/ffmpeg && \
    ./configure --prefix=/opt/ffmpeg --enable-gpl --enable-libx264 \
      --enable-shared --disable-static --disable-doc --disable-ffplay \
      --enable-ffmpeg --enable-ffprobe && \
    make -j$(nproc) && make install && \
    rm -rf /tmp/ffmpeg

ENV PKG_CONFIG_PATH=/opt/ffmpeg/lib/pkgconfig
ENV CGO_CFLAGS="-I/opt/ffmpeg/include"
ENV CGO_LDFLAGS="-L/opt/ffmpeg/lib -Wl,-rpath,/opt/ffmpeg/lib"

COPY go.mod go.sum* ./
RUN if [ -f go.sum ]; then go mod download; fi

COPY . .
RUN go build -o tstohls .

# ===========================================
# 第二阶段：运行镜像
# ===========================================
FROM alpine:latest

RUN apk add --no-cache ca-certificates tzdata

ENV TZ=Asia/Shanghai

WORKDIR /app

COPY --from=builder /opt/ffmpeg/lib /opt/ffmpeg/lib
COPY --from=builder /opt/ffmpeg/bin/ffmpeg /usr/bin/ffmpeg
COPY --from=builder /opt/ffmpeg/bin/ffprobe /usr/bin/ffprobe
RUN echo "/opt/ffmpeg/lib" > /etc/ld-musl-x86_64.path

COPY --from=builder /app/tstohls .
COPY --from=builder /app/web ./web

RUN mkdir -p ./data/logos && chmod -R 777 ./data

EXPOSE 15140

CMD ["./tstohls"]
