# Build stage
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata gcc musl-dev

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags="-w -s" -o tubarr ./cmd/tubarr


# Runtime stage
FROM alpine:3.20

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    ffmpeg \
    python3 \
    py3-pip \
    wget \
    sqlite-libs \
    su-exec && \
    wget -O /usr/local/bin/yt-dlp https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/yt-dlp && \
    chmod +x /usr/local/bin/yt-dlp

RUN wget -O /usr/local/bin/metarr https://github.com/TubarrApp/Metarr/releases/latest/download/metarr && \
    chmod +x /usr/local/bin/metarr

# Yt-dlp updater
RUN printf '#!/bin/sh\nwhile true; do\n  echo "[Updater] Checking for yt-dlp updates..."\n  yt-dlp -U > /dev/null 2>&1 || echo "[Updater] yt-dlp update failed."\n  echo "[Updater] Update check complete. Sleeping 24h..."\n  sleep 86400\ndone &\n' > /usr/local/bin/auto-updater \
 && chmod +x /usr/local/bin/auto-updater

# App files
WORKDIR /app
COPY --from=builder /build/tubarr /app/tubarr
COPY --from=builder /build/web /app/web
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

# Default runtime UID:GID (overridable)
ENV PUID=1000 PGID=1000

EXPOSE 8827

ENV TUBARR_HOME=/home/tubarr/.tubarr \
    TZ=UTC \
    PATH=/usr/local/bin:$PATH

VOLUME ["/home/tubarr/.tubarr", "/downloads", "/metadata"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8827/ || exit 1

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
