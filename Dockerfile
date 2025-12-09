# syntax=docker/dockerfile:1

###############################
# 1. Builder stage (Ubuntu)
###############################
FROM ubuntu:24.04 AS builder

ENV DEBIAN_FRONTEND=noninteractive

# Add multiverse and universe repos
RUN sed -i 's/^# deb .*universe/deb &/' /etc/apt/sources.list \
 && sed -i 's/^# deb-src .*universe/deb-src &/' /etc/apt/sources.list \
 && sed -i 's/^# deb .*multiverse/deb &/' /etc/apt/sources.list \
 && sed -i 's/^# deb-src .*multiverse/deb-src &/' /etc/apt/sources.list \
 && apt-get update

# Core build deps + Go toolchain deps
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    git \
    wget \
    build-essential \
    pkg-config \
    cmake \
    ninja-build \
    yasm \
    nasm \
    xz-utils \
    tzdata \
    sqlite3 libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

# Install Go manually (Ubuntu’s golang-go is too old)
RUN wget https://go.dev/dl/go1.25.0.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go1.25.0.linux-amd64.tar.gz && \
    rm go1.25.0.linux-amd64.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"
WORKDIR /build

############ FFmpeg build dependencies ############
RUN apt-get update && apt-get install -y --no-install-recommends \
    # Video codecs
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
    # Intel QSV / VPL (Arc-enabled)
    libvpl-dev \
    libva-dev \
    libva-drm2 \
    libdrm-dev \
    intel-media-va-driver-non-free \
    libmfx-gen1.2 \
    \
    # NVIDIA NVENC/NVDEC runtime libs
    libnvidia-encode-550 \
    libnvidia-decode-550 \
    \
    # FFprobe / misc
    zlib1g-dev \
    libbz2-dev \
    libxml2-dev \
    && rm -rf /var/lib/apt/lists/*

############ NVENC headers ############
RUN git clone https://github.com/FFmpeg/nv-codec-headers.git /tmp/nv && \
    make -C /tmp/nv install && rm -rf /tmp/nv

############ Build SVT-AV1 ############
RUN git clone --depth 1 https://gitlab.com/AOMediaCodec/SVT-AV1.git /tmp/svtav1 && \
    cd /tmp/svtav1 && mkdir build && cd build && \
    cmake -G Ninja -DCMAKE_BUILD_TYPE=Release .. && \
    ninja -j"$(nproc)" && ninja install && ldconfig && \
    rm -rf /tmp/svtav1

############ Build FFmpeg from source ############
RUN set -e && \
    rm -rf /ffmpeg && \
    git clone --depth 1 https://git.ffmpeg.org/ffmpeg.git /ffmpeg && \
    cd /ffmpeg && \
    PKG_CONFIG_PATH="/usr/lib/pkgconfig:/usr/local/lib/pkgconfig" ./configure \
        --prefix=/usr/local \
        --bindir=/usr/local/bin \
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
    && make -j"$(nproc)" \
    && make install \
    \
    # In case ffmpeg/ffprobe are built as *_g, normalize names
    && if [ -f /usr/local/bin/ffmpeg_g ]; then mv /usr/local/bin/ffmpeg_g /usr/local/bin/ffmpeg; fi \
    && if [ -f /usr/local/bin/ffprobe_g ]; then mv /usr/local/bin/ffprobe_g /usr/local/bin/ffprobe; fi \
    \
    # Verify installation (fail early if missing)
    && ls -la /usr/local/bin \
    && test -f /usr/local/bin/ffmpeg \
    && test -f /usr/local/bin/ffprobe \
    \
    # Strip (optional) and refresh ld cache
    && strip /usr/local/bin/ffmpeg /usr/local/bin/ffprobe || true \
    && find /usr/local/lib -name "*.so" -exec strip --strip-unneeded {} + \; || true \
    && ldconfig

# Extra hard fail if somehow broken in a later cached layer
RUN ls -la /usr/local/bin
RUN test -f /usr/local/bin/ffmpeg
RUN test -f /usr/local/bin/ffprobe

############ Build Tubarr ############
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags="-w -s" \
    -o tubarr ./cmd/tubarr

############ Build Metarr ############
RUN git clone https://github.com/TubarrApp/Metarr.git /build/metarr-src && \
    cd /build/metarr-src && \
    go mod download && \
    CGO_ENABLED=1 GOOS=linux go build -a -ldflags="-w -s" \
        -o /build/metarr ./cmd/metarr

###############################
# 2. Runtime Stage (Ubuntu)
###############################
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Add multiverse and universe repos
RUN sed -i 's/^# deb .*universe/deb &/' /etc/apt/sources.list \
 && sed -i 's/^# deb-src .*universe/deb-src &/' /etc/apt/sources.list \
 && sed -i 's/^# deb .*multiverse/deb &/' /etc/apt/sources.list \
 && sed -i 's/^# deb-src .*multiverse/deb-src &/' /etc/apt/sources.list \
 && apt-get update

RUN apt-get update && apt-get install -y --no-install-recommends \
    # Core utilities
    aria2 \
    axel \
    ca-certificates \
    python3 python3-pip \
    sqlite3 \
    gosu \
    tzdata \
    wget \
    xz-utils \
    # Runtime libraries matching FFmpeg build extras
    # Audio codecs
    libmp3lame0 \
    libopus0 \
    libvorbis0a \
    libvorbisenc2 \
    libfdk-aac2 \
    libflac12 \
    \
    # Video codecs
    libx264-164 \
    libx265-199 \
    libvpx9 \
    libdav1d7 \
    \
    # Subtitle / text rendering (required by libass)
    libass9 \
    libfreetype6 \
    libfribidi0 \
    libharfbuzz0b \
    libfontconfig1 \
    \
    # Compression and misc libs needed by ffprobe
    zlib1g \
    libbz2-1.0 \
    libxml2 \
    # Intel QSV / VAAPI runtime stack
    intel-media-va-driver-non-free \
    libigdgmm12 \
    libvpl2 \
    libva2 \
    libva-drm2 \
    mesa-va-drivers \
    libdrm2 \
    libze-intel-gpu1 \
    libmfx-gen1.2 \
    # NVIDIA NVENC/NVDEC runtime stack
    libnvidia-encode-550 \
    libnvidia-decode-550 \
    # Cleanup
    && rm -rf /var/lib/apt/lists/*

# Fix /tmp permissions for non-root usage
RUN chmod 1777 /tmp

######## Install FFmpeg runtime ########
COPY --from=builder /usr/local/bin/ffmpeg /usr/local/bin/ffmpeg
COPY --from=builder /usr/local/bin/ffprobe /usr/local/bin/ffprobe
COPY --from=builder /usr/local/lib/ /usr/local/lib/
RUN ldconfig

######## Install Deno globally ########
RUN apt-get update && apt-get install -y unzip && rm -rf /var/lib/apt/lists/* && \
    wget -O /tmp/deno.zip \
        https://github.com/denoland/deno/releases/latest/download/deno-x86_64-unknown-linux-gnu.zip && \
    unzip -o /tmp/deno.zip -d /usr/bin && \
    chmod +x /usr/bin/deno && \
    rm /tmp/deno.zip

######## Install yt-dlp ########
RUN wget -O /usr/local/bin/yt-dlp \
        https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/yt-dlp \
    && chmod +x /usr/local/bin/yt-dlp

######## Copy Tubarr + Metarr ########
COPY --from=builder /build/tubarr /app/tubarr
COPY --from=builder /build/metarr /usr/local/bin/metarr
RUN chmod +x /app/tubarr /usr/local/bin/metarr

######## Auto-updater ########
RUN chmod 755 /usr/local/bin/yt-dlp
RUN printf '%s\n' \
'#!/bin/sh' \
'while true; do' \
'  yt-dlp -U > /dev/null 2>&1' \
'  sleep 86400' \
'done &' \
> /usr/local/bin/auto-updater && chmod +x /usr/local/bin/auto-updater

######## App files ########
WORKDIR /app
COPY --from=builder /build/web /app/web
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

######## Cleanup ########
# Delete unnecessary elements
RUN apt-get purge -y python3-pip && \
    apt-get autoremove -y && \
    rm -rf /root/.cache
RUN rm -rf /usr/share/man/* /usr/share/doc/* /usr/share/local/* || true
RUN find /usr/share/locale -mindepth 1 -maxdepth 1 ! -name "en" -exec rm -rf {} +
RUN rm -rf /var/lib/apt/lists/* /var/cache/apt/* /var/cache/debconf/*-old
RUN rm -rf /usr/share/icons /usr/share/pixmaps
RUN rm -rf /usr/share/info /usr/share/lintian /usr/share/linda
RUN rm -rf /usr/lib/python3*/test
RUN rm -rf /usr/share/perl* /usr/lib/perl*
RUN rm -rf /usr/share/ruby /usr/lib/ruby

######## User Logic ########
RUN userdel -r ubuntu 2>/dev/null || true && \
    groupadd -g 1000 tubarr && \
    useradd -u 1000 -g tubarr -d /home/tubarr -s /bin/bash -m tubarr

RUN mkdir -p /home/tubarr/.tubarr /downloads /metadata && \
    chown -R tubarr:tubarr /home/tubarr /downloads /metadata

ENV PUID=1000 PGID=1000
ENV HOME=/home/tubarr
ENV TZ=UTC
ENV PATH="/usr/local/bin:${PATH}"

EXPOSE 8827
VOLUME ["/home/tubarr/.tubarr", "/downloads", "/metadata"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8827/ || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]