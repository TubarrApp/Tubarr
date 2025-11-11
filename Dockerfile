# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /build

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=1 GOOS=linux go build -a -ldflags="-w -s" -o tubarr ./cmd/tubarr

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    ffmpeg \
    python3 \
    py3-pip \
    wget \
    git \
    go \
    gcc \
    musl-dev \
    sqlite-libs \
    && pip3 install --break-system-packages yt-dlp

# Build and install Metarr from source
RUN cd /tmp && \
    git clone https://github.com/TubarrApp/Metarr.git && \
    cd Metarr && \
    go build -o /usr/local/bin/metarr ./cmd/metarr && \
    chmod +x /usr/local/bin/metarr && \
    cd / && \
    rm -rf /tmp/Metarr

# Create app user and directories
RUN addgroup -g 1000 tubarr && \
    adduser -D -u 1000 -G tubarr tubarr && \
    mkdir -p /app /config /downloads /metadata /app/web/dist && \
    chown -R tubarr:tubarr /app /config /downloads /metadata

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/tubarr /app/tubarr

# Copy web files
COPY --from=builder /build/web/dist /app/web/dist

# Set permissions
RUN chmod +x /app/tubarr

# Switch to non-root user
USER tubarr

# Expose web server port (default Tubarr port)
EXPOSE 8827

# Set environment variables
ENV TUBARR_HOME=/config \
    TZ=UTC \
    PATH=/usr/local/bin:$PATH

# Volume mounts for persistent data
VOLUME ["/config", "/downloads", "/metadata"]

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8827/ || exit 1

# Run Tubarr with --web flag
ENTRYPOINT ["/app/tubarr"]
CMD ["--web"]
