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

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags="-w -s" \
    -o tubarr ./cmd/tubarr

RUN git clone https://github.com/TubarrApp/Metarr.git /build/metarr-src \
 && cd /build/metarr-src \
 && go mod download \
 && CGO_ENABLED=1 GOOS=linux go build -a -ldflags="-w -s" \
      -o /build/metarr ./cmd/metarr

# --- Runtime stage -----------------------------------------------------------
FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
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
        \
        # VAAPI runtime libs (for Intel GPU acceleration)
        intel-media-va-driver \
        libva-drm2 \
        libva2 \
    && rm -rf /var/lib/apt/lists/*

# Download and install BtbN FFmpeg build
# Using latest release - you can pin to a specific version if needed
RUN wget -O ffmpeg.tar.xz \
    "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-linux64-gpl-shared.tar.xz" \
 && tar -xf ffmpeg.tar.xz \
 && cd ffmpeg-master-latest-linux64-gpl-shared \
 && cp -r bin/* /usr/local/bin/ \
 && cp -r lib/* /usr/local/lib/ \
 && cp -r include/* /usr/local/include/ \
 && ldconfig \
 && cd .. \
 && rm -rf ffmpeg.tar.xz ffmpeg-master-latest-linux64-gpl-shared

# yt-dlp
RUN wget -O /usr/local/bin/yt-dlp \
        https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/yt-dlp \
    && chmod +x /usr/local/bin/yt-dlp

# Copy binaries
COPY --from=builder /build/tubarr /app/tubarr
COPY --from=builder /build/metarr /usr/local/bin/metarr
RUN chmod +x /app/tubarr /usr/local/bin/metarr

# yt-dlp auto-updater
RUN printf '%s\n' \
'#!/bin/sh' \
'while true; do' \
'  yt-dlp -U > /dev/null 2>&1 || true' \
'  sleep 86400' \
'done &' \
> /usr/local/bin/auto-updater \
 && chmod +x /usr/local/bin/auto-updater

WORKDIR /app
COPY --from=builder /build/web /app/web
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENV PUID=1000 PGID=1000
ENV TUBARR_HOME=/home/tubarr/.tubarr
ENV HOME=/home/tubarr
ENV TZ=UTC

EXPOSE 8827

VOLUME ["/home/tubarr/.tubarr", "/downloads", "/metadata"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8827/ || exit 1

RUN mkdir -p /home/tubarr /downloads /metadata
ENV PATH="/usr/local/bin:${PATH}"

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]