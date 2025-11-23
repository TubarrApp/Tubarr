# syntax=docker/dockerfile:1

FROM jrottenberg/ffmpeg:7.1.2-scratch320 AS ffmpeg_full

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

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    wget \
    python3 python3-pip \
    sqlite3 \
    su-exec \
    aria2 \
    axel \
    libva2 libva-drm2 libva-x11-2 \
    mesa-va-drivers \
    intel-media-va-driver-non-free \
    libvpl2 libmfx1 \
    intel-opencl-icd \
    libnvidia-encode1 libnvidia-decode1 \
    v4l-utils \
    libdrm2 \
    udev \
    && rm -rf /var/lib/apt/lists/*

# yt-dlp download
RUN wget -O /usr/local/bin/yt-dlp \
        https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/yt-dlp \
    && chmod +x /usr/local/bin/yt-dlp

# Copy built binaries from the builder
COPY --from=builder /build/tubarr /app/tubarr
COPY --from=builder /build/metarr /usr/local/bin/metarr

# Copy ffmpeg from build stage
COPY --from=ffmpeg_full /usr/local/bin/ffmpeg /usr/local/bin/
COPY --from=ffmpeg_full /usr/local/bin/ffprobe /usr/local/bin/

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
