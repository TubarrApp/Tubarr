# syntax=docker/dockerfile:1

# --- Build stage -------------------------------------------------------------
FROM golang:1.25-bookworm AS builder

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

FROM debian:bookworm-slim

RUN set -eux; \
    printf '%s\n' \
      "deb http://deb.debian.org/debian bookworm main contrib non-free non-free-firmware" \
      "deb http://deb.debian.org/debian-security bookworm-security main contrib non-free non-free-firmware" \
      "deb http://deb.debian.org/debian bookworm-updates main contrib non-free non-free-firmware" \
      > /etc/apt/sources.list; \
    apt-get update || (echo "APT ERROR — SHOWING LOGS:" && cat /var/log/apt/* && exit 1); \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        aria2 \
        axel \
        ca-certificates \
        intel-media-va-driver-non-free \
        libsqlite3-0 \
        libva2 \
        mesa-va-drivers \
        python3 python3-pip \
        sqlite3 \
        gosu \
        tzdata \
        wget \
        xz-utils \
        || (echo "APT ERROR — SHOWING LOGS:" && cat /var/log/apt/* && exit 1); \
    rm -rf /var/lib/apt/lists/*

# Install Jellyfin ffmpeg (Debian bookworm build with hardware acceleration)
RUN set -eux; \
    rm -f /etc/apt/sources.list.d/debian.sources; \
    printf '%s\n' \
      "deb http://deb.debian.org/debian bookworm main contrib non-free non-free-firmware" \
      "deb http://deb.debian.org/debian-security bookworm-security main contrib non-free non-free-firmware" \
      "deb http://deb.debian.org/debian bookworm-updates main contrib non-free non-free-firmware" \
      > /etc/apt/sources.list; \
    apt-get update; \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        wget \
        xz-utils \
        ca-certificates; \
    wget -O /tmp/jellyfin-ffmpeg.deb \
        https://github.com/jellyfin/jellyfin-ffmpeg/releases/download/v7.1.2-4/jellyfin-ffmpeg7_7.1.2-4-bookworm_amd64.deb; \
    apt-get install -y --no-install-recommends /tmp/jellyfin-ffmpeg.deb; \
    ln -sf /usr/lib/jellyfin-ffmpeg/ffmpeg /usr/bin/ffmpeg; \
    ln -sf /usr/lib/jellyfin-ffmpeg/ffprobe /usr/bin/ffprobe; \
    rm -f /tmp/jellyfin-ffmpeg.deb; \
    rm -rf /var/lib/apt/lists/*

# yt-dlp download
RUN wget -O /usr/local/bin/yt-dlp \
        https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/yt-dlp \
    && chmod +x /usr/local/bin/yt-dlp

# Copy built binaries from the builder
COPY --from=builder /build/tubarr /app/tubarr
COPY --from=builder /build/metarr /usr/local/bin/metarr

# Fix permissions
RUN chmod +x /app/tubarr /usr/local/bin/metarr

# yt-dlp auto-updater
RUN printf '%s\n' \
'#!/bin/sh' \
'while true; do' \
'  echo "[Updater] Checking for yt-dlp updates..."' \
'  yt-dlp -U > /dev/null 2>&1 || echo "[Updater] yt-dlp update failed."' \
'  echo "[Updater] Update check complete. Sleeping 24h..."' \
'  sleep 86400' \
'done &' \
> /usr/local/bin/auto-updater \
 && chmod +x /usr/local/bin/auto-updater

# App files
WORKDIR /app
COPY --from=builder /build/web /app/web
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENV PUID=1000 PGID=1000
ENV TUBARR_HOME=/home/tubarr/.tubarr
ENV TZ=UTC

EXPOSE 8827

VOLUME ["/home/tubarr/.tubarr", "/downloads", "/metadata"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8827/ || exit 1

RUN mkdir -p /home/tubarr
RUN mkdir -p /downloads /metadata
ENV PATH="/usr/local/bin:${PATH}"
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
