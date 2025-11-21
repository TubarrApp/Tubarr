# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata gcc musl-dev

WORKDIR /build

# Copy go mod files and download deps
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build Tubarr with CGO enabled for SQLite
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
    sqlite-libs && \
    wget -O /usr/local/bin/yt-dlp https://github.com/yt-dlp/yt-dlp-nightly-builds/releases/latest/download/yt-dlp && \
    chmod +x /usr/local/bin/yt-dlp

# Download Metarr binary from latest release
RUN wget -O /usr/local/bin/metarr https://github.com/TubarrApp/Metarr/releases/latest/download/metarr && \
    chmod +x /usr/local/bin/metarr

# Add background updater (yt-dlp only - Metarr version is pinned to Tubarr release)
RUN echo '#!/bin/sh\n\
while true; do\n\
  echo "[Updater] Checking for yt-dlp updates..."\n\
  yt-dlp -U > /dev/null 2>&1 || echo "[Updater] yt-dlp update failed."\n\
  echo "[Updater] Update check complete. Sleeping 24h..."\n\
  sleep 86400\n\
done &' > /usr/local/bin/auto-updater && chmod +x /usr/local/bin/auto-updater

# Create app user and dirs
RUN addgroup -g 1000 tubarr && \
    adduser -D -u 1000 -G tubarr tubarr && \
    mkdir -p /home/tubarr/.tubarr /downloads /metadata && \
    chown -R tubarr:tubarr /home/tubarr /downloads /metadata

WORKDIR /app
COPY --from=builder /build/tubarr /app/tubarr
COPY --from=builder /build/web /app/web
RUN chmod +x /app/tubarr

USER tubarr

EXPOSE 8827

ENV TUBARR_HOME=/home/tubarr/.tubarr \
    TZ=UTC \
    PATH=/usr/local/bin:$PATH

VOLUME ["/home/tubarr/.tubarr", "/downloads", "/metadata"]

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8827/ || exit 1

ENTRYPOINT ["/bin/sh", "-c", "/usr/local/bin/auto-updater & exec /app/tubarr --web"]
