# syntax=docker/dockerfile:1

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

FROM jrottenberg/ffmpeg:7.1-ubuntu2404-edge

RUN apt-get update || (echo "APT ERROR — SHOWING LOGS:" && cat /var/log/apt/* && exit 1); \
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

RUN wget -O /usr/local/bin/yt-dlp \
        https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/yt-dlp \
    && chmod +x /usr/local/bin/yt-dlp

COPY --from=builder /build/tubarr /app/tubarr
COPY --from=builder /build/metarr /usr/local/bin/metarr

RUN chmod +x /app/tubarr /usr/local/bin/metarr

RUN printf '%s\n' \
'#!/bin/sh' \
'while true; do' \
'  yt-dlp -U > /dev/null 2>&1' \
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
ENV TZ=UTC

EXPOSE 8827

VOLUME ["/home/tubarr/.tubarr", "/downloads", "/metadata"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:8827/ || exit 1

RUN mkdir -p /home/tubarr
RUN mkdir -p /downloads /metadata
ENV PATH="/usr/local/bin:${PATH}"
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
