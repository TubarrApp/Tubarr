# syntax=docker/dockerfile:1

# --- Build stage -------------------------------------------------------------
FROM golang:1.25 AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    ca-certificates \
    tzdata \
    build-essential \
    pkg-config \
    sqlite3 libsqlite3-dev \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /build

# Download deps
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build Tubarr (CGO enabled)
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags="-w -s" \
    -o tubarr ./cmd/tubarr

# Build Metarr
RUN git clone https://github.com/TubarrApp/Metarr.git /build/metarr-src \
 && cd /build/metarr-src \
 && go mod download \
 && CGO_ENABLED=1 GOOS=linux go build -a -ldflags="-w -s" \
      -o /build/metarr ./cmd/metarr

# --- Runtime stage -----------------------------------------------------------

FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        aria2 \
        axel \
        ca-certificates \
        libsqlite3-0 \
        python3 python3-pip \
        sqlite3 \
        gosu \
        tzdata \
        wget \
        xz-utils \
        git \
        build-essential \
        pkg-config \
        yasm \
        nasm \
        \
        # Video encoders / decoders
        libx264-dev \
        libx265-dev \
        libvpx-dev \
        libsvtav1-dev \
        libdav1d-dev \
        libmp3lame-dev \
        libopus-dev \
        libvorbis-dev \
        libflac-dev \
        libfdk-aac-dev \
        libass-dev \
        \
        # Intel QSV (modern – required)
        libvpl-dev \
        libva-dev \
        libva-drm2 \
        libdrm-dev \
        intel-media-va-driver \
        \
        # NVIDIA NVENC/NVDEC
        libnvidia-encode-550 \
        libnvidia-decode-550 \
        \
    && rm -rf /var/lib/apt/lists/*

# NVIDIA NVENC/NVDEC headers
RUN git clone https://github.com/FFmpeg/nv-codec-headers.git /tmp/nv && \
    make -C /tmp/nv install && rm -rf /tmp/nv

# Dependencies needed to build SVT-AV1 from source
RUN apt-get update && apt-get install -y --no-install-recommends \
    cmake \
    ninja-build \
    yasm \
    nasm \
    build-essential \
    pkg-config \
    && rm -rf /var/lib/apt/lists/*

# Build SVT-AV1 from source (required for FFmpeg)
RUN git clone --depth 1 https://gitlab.com/AOMediaCodec/SVT-AV1.git /tmp/svtav1 && \
    cd /tmp/svtav1 && \
    mkdir build && cd build && \
    cmake -G Ninja -DCMAKE_BUILD_TYPE=Release .. && \
    ninja -j"$(nproc)" && ninja install && ldconfig && \
    rm -rf /tmp/svtav1

# Build FFmpeg from current master (required for VPL)
RUN git clone --depth 1 https://git.ffmpeg.org/ffmpeg.git /ffmpeg && \
    cd /ffmpeg && \
    PKG_CONFIG_PATH="/usr/lib/pkgconfig:/usr/local/lib/pkgconfig" ./configure \
        --prefix=/usr/local \
        --enable-gpl \
        --enable-nonfree \
        --enable-shared \
        --disable-debug \
        \
        --enable-libx264 \
        --enable-libx265 \
        --enable-libvpx \
        --enable-libsvtav1 \
        --enable-libdav1d \
        \
        --enable-libmp3lame \
        --enable-libopus \
        --enable-libvorbis \
        --enable-libfdk-aac \
        --enable-libass \
        \
        --enable-vaapi \
        --enable-libdrm \
        --enable-libvpl \
        \
        --enable-nvenc \
        --enable-nvdec \
    && make -j"$(nproc)" && make install && ldconfig && \
    rm -rf /ffmpeg

# yt-dlp download
RUN wget -O /usr/local/bin/yt-dlp \
        https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/yt-dlp \
    && chmod +x /usr/local/bin/yt-dlp

# Copy built binaries from the builder
COPY --from=builder /build/tubarr /app/tubarr
COPY --from=builder /build/metarr /usr/local/bin/metarr
RUN chmod +x /app/tubarr /usr/local/bin/metarr

# yt-dlp auto-updater
RUN printf '%s\n' \
'#!/bin/sh' \
'while true; do' \
'  yt-dlp -U > /dev/null 2>&1' \
'  sleep 86400' \
'done &' \
> /usr/local/bin/auto-updater \
 && chmod +x /usr/local/bin/auto-updater

# App files
WORKDIR /app
COPY --from=builder /build/web /app/web
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Remove ubuntu user if it exists, then create tubarr user
RUN userdel -r ubuntu 2>/dev/null || true \
    && groupadd -g 1000 tubarr \
    && useradd -u 1000 -g tubarr -d /home/tubarr -s /bin/bash -m tubarr

# Create necessary directories with proper ownership
RUN mkdir -p /home/tubarr/.tubarr /downloads /metadata \
    && chown -R tubarr:tubarr /home/tubarr /downloads /metadata

ENV PUID=1000 PGID=1000
ENV HOME=/home/tubarr
ENV TZ=UTC

EXPOSE 8827

VOLUME ["/home/tubarr/.tubarr", "/downloads", "/metadata"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8827/ || exit 1

RUN mkdir -p /home/tubarr /downloads /metadata
ENV PATH="/usr/local/bin:${PATH}"

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
