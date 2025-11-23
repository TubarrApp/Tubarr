# syntax=docker/dockerfile:1

# --- Build stage -------------------------------------------------------------

FROM golang:1.25-alpine AS builder

RUN apk add --no-cache \
    git \
    ca-certificates \
    tzdata \
    gcc \
    musl-dev \
    sqlite-dev

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

FROM alpine:3.20

RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    python3 \
    py3-pip \
    wget \
    sqlite-libs \
    su-exec \
    aria2 \
    axel \
    gcompat \
    mesa \
    mesa-va-gallium \
    intel-media-driver \
    libva \
    libva-intel-driver

# Install Jellyfin ffmpeg portable (static build with full hardware acceleration)
RUN wget -O /tmp/jellyfin-ffmpeg.tar.xz \
    https://github.com/jellyfin/jellyfin-ffmpeg/releases/download/v7.0.2-5/jellyfin-ffmpeg_7.0.2-5_portable_linux64-gpl.tar.xz \
 && mkdir -p /opt/jellyfin-ffmpeg \
 && tar -xf /tmp/jellyfin-ffmpeg.tar.xz -C /opt/jellyfin-ffmpeg \
 && rm /tmp/jellyfin-ffmpeg.tar.xz \
 && find /opt/jellyfin-ffmpeg -name ffmpeg -type f -exec ln -sf {} /usr/bin/ffmpeg \; \
 && find /opt/jellyfin-ffmpeg -name ffprobe -type f -exec ln -sf {} /usr/bin/ffprobe \;

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
