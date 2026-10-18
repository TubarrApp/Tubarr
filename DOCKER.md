# Tubarr Docker Deployment Guide

This guide explains how to run Tubarr in Docker or Podman containers.

## Prerequisites

- **Docker** (version 20.10+) OR **Podman** (version 3.0+)
- **Docker Compose** (optional, for easier deployment)
- At least 2GB of available disk space

## Quick Start with Docker Compose

1. **Copy [`examples/docker-compose.yaml`](examples/docker-compose.yaml)** into a folder of your own, as `docker-compose.yaml`. It uses the published `ghcr.io/tubarrapp/tubarr` image, so there's nothing to build.

2. **Edit it:**
   - Set the host paths (left of each `:`) for Tubarr's data and your video library, and create those folders, owned by `PUID`/`PGID`
   - Set `TZ`, and `PUID`/`PGID` to the user that owns those folders
   - Uncomment the GPU device or the optional web settings if you want them

3. **Start Tubarr:**
   ```bash
   docker compose up -d
   ```

4. **Access the web interface:**
   - Open your browser to `http://localhost:8827`

5. **View logs:**
   ```bash
   docker compose logs -f tubarr
   ```

6. **Stop Tubarr:**
   ```bash
   docker compose down
   ```

For Podman, [`examples/podman-quadlet.container`](examples/podman-quadlet.container) is the Quadlet equivalent.

## Manual Docker Build and Run

### Building the Image

To build from a local checkout instead of using the published image (then use `image: tubarr:latest` in your compose file):

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
  -v $(pwd)/.tubarr:/home/tubarr/.tubarr \
  -e TZ=America/New_York \
  --restart unless-stopped \
  tubarr:latest
```

#### Podman:
```bash
podman run -d \
  --name tubarr \
  -p 8827:8827 \
  -v $(pwd)/.tubarr:/home/tubarr/.tubarr:Z \
  -e TZ=America/New_York \
  --restart unless-stopped \
  tubarr:latest
```

**Note:** Podman requires `:Z` suffix on volume mounts for SELinux contexts.

## Volume Mounts

| Volume Path | Purpose | Required |
|------------|---------|----------|
| `/home/tubarr/.tubarr` | Database, settings, and configuration files | Yes |
| `/home/tubarr/.metarr` | Metarr's logs and data | Recommended |
| e.g. `/videos` | Your video library | Yes, for downloads |

**Note:** Channels' video and metadata directories are paths *inside the container*, so mount your library (e.g. at `/videos`) and set channels to save under it (e.g. `/videos/{{channel_name}}`). Avoid mounting a large library at `/downloads` or `/metadata`: the container changes ownership of everything under those two paths to `PUID`/`PGID` on every start.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `TZ` | `UTC` | Timezone for logs and scheduling |
| `PUID` | `1000` | User ID for running Tubarr (matches host user to avoid permission issues) |
| `PGID` | `1000` | Group ID for running Tubarr (matches host group to avoid permission issues) |
| `TUBARR_WEB_ADDRESS` | `:8827` | Address the web interface listens on inside the container |
| `TUBARR_WEB_AUTH` | (Settings page) | Overrides the login mode in Settings: `required`, `local` (no login from inside the container) or `none`, e.g. behind a reverse proxy with its own login |
| `TUBARR_WEB_HTTPS` | | `true` or `false`, overriding the **Use HTTPS** setting. Uses a self-signed certificate unless one is set below or in Settings |
| `TUBARR_WEB_TLS_CERT` | | Path inside the container to a PEM certificate (or certificate and key combined), e.g. a read-only mount of `/certs/fullchain.pem` |
| `TUBARR_WEB_TLS_KEY` | | Path to the PEM private key, if not combined with the certificate |
| `TUBARR_WEB_USERNAME` / `TUBARR_WEB_PASSWORD` | | Sets the web login at every start while set, e.g. to reset a forgotten password. The password alone keeps the saved username, but both are needed when no login exists yet |
| `TUBARR_WEB_ALLOWED_HOSTS` | | Comma-separated hostnames allowed alongside those under **Settings > Web Interface Access**, e.g. a reverse proxy's when the login is off |

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
docker compose pull
docker compose up -d
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
-v ./.tubarr:/home/tubarr/.tubarr:Z
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

The image includes these dependencies. If they're missing, pull the image again (`docker compose pull`), or rebuild a local image with `docker build --no-cache -t tubarr:latest .`.

## Accessing the Container Shell

Useful for debugging:

```bash
# Docker
docker exec -it tubarr sh

# Podman
podman exec -it tubarr sh
```

## Health Check

The container includes a health check that runs every 30 seconds against `http://localhost:8827/health` (or `https://` when HTTPS is on). It returns 200 when the server and database are working, and 503 otherwise:

```bash
# Docker
docker inspect tubarr | grep -A 10 Health

# Podman
podman inspect tubarr | grep -A 10 Healthcheck
```

## Advanced Configuration

### Using a Custom Command

Arguments after the image name replace the default `--web`, so include it to keep the web interface running:

```bash
docker run -d \
  --name tubarr \
  -p 8827:8827 \
  -v $(pwd)/.tubarr:/home/tubarr/.tubarr \
  tubarr:latest --web --debug 3
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

1. The container runs as the non-root user `tubarr`, with the `PUID` and `PGID` IDs (1000 by default)
2. No unnecessary packages are installed
3. All volumes should use restrictive permissions on the host
4. Consider using Docker secrets for sensitive configuration
5. The web interface asks for a login, created on the first visit. To reset a forgotten password, run `docker exec -u tubarr tubarr tubarr auth reset` and create a new login on the next visit, or set `TUBARR_WEB_PASSWORD` in the compose file and restart (add `TUBARR_WEB_USERNAME` to change the username too). It's applied at every start while set, so remove it afterwards to manage the login in Settings.
6. Requests from the host reach the container from the Docker gateway (e.g. `172.17.0.1`), not localhost. Listing the whole Docker range under **Skip Login For** skips the login for everything reaching the published port.

## Backup and Restore

Paths below are the example compose file's; use your own host data folder.

### Backup:
```bash
# Stop the container
docker compose down

# Backup the data folder (includes the database)
tar -czf tubarr-backup-$(date +%Y%m%d).tar.gz -C /opt/containers/tubarr .tubarr

# Restart
docker compose up -d
```

### Restore:
```bash
# Stop the container
docker compose down

# Restore from backup
tar -xzf tubarr-backup-YYYYMMDD.tar.gz -C /opt/containers/tubarr

# Restart
docker compose up -d
```

## Network Configuration

Compose puts Tubarr on its project's default network. To reach it from containers in another compose project (e.g. a reverse proxy), give both a shared network:

```yaml
services:
  tubarr:
    networks:
      - proxy

networks:
  proxy:
    external: true # Created once with: docker network create proxy
```

## Support

For issues specific to Docker deployment, please check:
- Container logs: `docker logs tubarr` or `podman logs tubarr`
- Host system logs: `journalctl -u docker` or `journalctl --user -u tubarr`
- GitHub Issues: https://github.com/TubarrApp/Tubarr/issues
