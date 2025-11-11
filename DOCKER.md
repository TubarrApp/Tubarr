# Tubarr Docker Deployment Guide

This guide explains how to run Tubarr in Docker or Podman containers.

## Prerequisites

- **Docker** (version 20.10+) OR **Podman** (version 3.0+)
- **Docker Compose** (optional, for easier deployment)
- At least 2GB of available disk space

## Quick Start with Docker Compose

1. **Clone the repository:**
   ```bash
   git clone https://github.com/YourUsername/Tubarr.git
   cd Tubarr
   ```

2. **Edit docker-compose.yml (optional):**
   - Change the timezone in the `TZ` environment variable
   - Adjust volume mount paths if needed
   - Modify resource limits if desired

3. **Start Tubarr:**
   ```bash
   docker-compose up -d
   ```

4. **Access the web interface:**
   - Open your browser to `http://localhost:8827`

5. **View logs:**
   ```bash
   docker-compose logs -f tubarr
   ```

6. **Stop Tubarr:**
   ```bash
   docker-compose down
   ```

## Manual Docker Build and Run

### Building the Image

```bash
# Build with Docker
docker build -t tubarr:latest .

# Or build with Podman
podman build -t tubarr:latest .
```

### Running the Container

#### Docker:
```bash
docker run -d \
  --name tubarr \
  -p 8827:8827 \
  -v $(pwd)/tubarr-config:/config \
  -v $(pwd)/tubarr-downloads:/downloads \
  -v $(pwd)/tubarr-metadata:/metadata \
  -e TZ=America/New_York \
  --restart unless-stopped \
  tubarr:latest
```

#### Podman:
```bash
podman run -d \
  --name tubarr \
  -p 8827:8827 \
  -v $(pwd)/tubarr-config:/config:Z \
  -v $(pwd)/tubarr-downloads:/downloads:Z \
  -v $(pwd)/tubarr-metadata:/metadata:Z \
  -e TZ=America/New_York \
  --restart unless-stopped \
  tubarr:latest
```

**Note:** Podman requires `:Z` suffix on volume mounts for SELinux contexts.

## Volume Mounts

The container uses three main volumes:

| Volume Path | Purpose | Required |
|------------|---------|----------|
| `/config` | Database, settings, and configuration files | Yes |
| `/downloads` | Downloaded video files | Yes |
| `/metadata` | JSON metadata files | Yes |

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `TUBARR_HOME` | `/config` | Configuration directory path |
| `TZ` | `UTC` | Timezone for logs and scheduling |

## Port Mapping

| Container Port | Purpose | Default Host Port |
|---------------|---------|-------------------|
| `8827` | Web interface | `8827` |

## Running with Podman Compose

Podman has its own compose implementation:

```bash
# Start with podman-compose
podman-compose up -d

# View logs
podman-compose logs -f

# Stop
podman-compose down
```

## Systemd Service (Podman)

To run Tubarr as a systemd service with Podman:

1. **Generate the systemd unit file:**
   ```bash
   podman generate systemd --new --name tubarr > ~/.config/systemd/user/tubarr.service
   ```

2. **Enable and start the service:**
   ```bash
   systemctl --user enable tubarr.service
   systemctl --user start tubarr.service
   ```

3. **Check status:**
   ```bash
   systemctl --user status tubarr.service
   ```

## Updating Tubarr

### With Docker Compose:
```bash
docker-compose down
docker-compose build --no-cache
docker-compose up -d
```

### With Docker:
```bash
docker stop tubarr
docker rm tubarr
docker build -t tubarr:latest .
# Then run the container again
```

### With Podman:
```bash
podman stop tubarr
podman rm tubarr
podman build -t tubarr:latest .
# Then run the container again
```

## Troubleshooting

### Container won't start

Check logs:
```bash
# Docker
docker logs tubarr

# Podman
podman logs tubarr
```

### Permission issues (Podman with SELinux)

Add `:Z` to volume mounts:
```bash
-v ./tubarr-config:/config:Z
```

### Port already in use

Change the host port mapping:
```bash
-p 9999:8827  # Access via http://localhost:9999
```

### Database locked errors

Ensure only one instance of Tubarr is running:
```bash
# Docker
docker ps | grep tubarr

# Podman
podman ps | grep tubarr
```

### yt-dlp or Metarr not found

The Dockerfile includes these dependencies. If they're missing, rebuild:
```bash
docker-compose build --no-cache
```

## Accessing the Container Shell

Useful for debugging:

```bash
# Docker
docker exec -it tubarr sh

# Podman
podman exec -it tubarr sh
```

## Health Check

The container includes a health check that runs every 30 seconds:

```bash
# Docker
docker inspect tubarr | grep -A 10 Health

# Podman
podman inspect tubarr | grep -A 10 Healthcheck
```

## Advanced Configuration

### Using a Custom Command

To run Tubarr with different flags:

```bash
docker run -d \
  --name tubarr \
  -p 8827:8827 \
  -v $(pwd)/tubarr-config:/config \
  tubarr:latest --web --some-other-flag
```

### Resource Limits

#### Docker Compose:
```yaml
deploy:
  resources:
    limits:
      cpus: '2.0'
      memory: 4G
    reservations:
      cpus: '1.0'
      memory: 2G
```

#### Docker CLI:
```bash
docker run -d \
  --cpus="2.0" \
  --memory="4g" \
  --name tubarr \
  tubarr:latest
```

## Security Considerations

1. The container runs as non-root user `tubarr` (UID 1000)
2. No unnecessary packages are installed
3. All volumes should use restrictive permissions on the host
4. Consider using Docker secrets for sensitive configuration

## Backup and Restore

### Backup:
```bash
# Stop the container
docker-compose down

# Backup config directory (includes database)
tar -czf tubarr-backup-$(date +%Y%m%d).tar.gz tubarr-config/

# Restart
docker-compose up -d
```

### Restore:
```bash
# Stop the container
docker-compose down

# Restore from backup
tar -xzf tubarr-backup-YYYYMMDD.tar.gz

# Restart
docker-compose up -d
```

## Network Configuration

The docker-compose.yml creates a dedicated bridge network. To connect other containers:

```yaml
services:
  your-service:
    networks:
      - tubarr-network

networks:
  tubarr-network:
    external: true
```

## Support

For issues specific to Docker deployment, please check:
- Container logs: `docker logs tubarr` or `podman logs tubarr`
- Host system logs: `journalctl -u docker` or `journalctl --user -u tubarr`
- GitHub Issues: https://github.com/YourUsername/Tubarr/issues
