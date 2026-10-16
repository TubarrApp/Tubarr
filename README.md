# Tubarr

Tubarr is a Go-powered companion to Metarr that keeps long-form video libraries fresh. It crawls creator channels, downloads the episodes with `yt-dlp`, applies your naming/metadata rules through Metarr, and can refresh Plex/Emby/Jellyfin libraries automatically—all from the CLI or an optional web UI.

---

## Features

- **Channel-aware crawler** – schedule scans per channel, respect pauses, and avoid bot detection with randomized wait windows.
- **Config-driven downloads** – store channel settings in Viper-compatible files (YAML/TOML/JSON) and batch-import them.
- **Metarr integration** – share templated directories, filename/meta operations, output routing, and run the Metarr CLI automatically when available.
- **Choose what to download** – channels (or channel URLs) that don't download automatically list new videos under **Available** in the web UI for you to pick from.
- **Web dashboard** – start Tubarr in web mode (`tubarr --web`) for a UI that manages channels, downloads, transcodes, logs, and settings, and crawls channels on schedule.
- **Notifications & hooks** – call arbitrary URLs (e.g., Plex library refresh) when new videos land.
- **Docker-friendly** – ships with a Dockerfile, Compose files, and a Podman Quadlet example for unattended deployments.

---

## Requirements

- `yt-dlp` on `PATH`
- Optional: `metarr` (and FFmpeg/FFprobe, which Metarr uses) for tagging, transcoding, outputting to directories, etc.

(All included in the container image)

---

## Container Note

The Tubarr container image is a little over a gigabyte, making it larger than images of similar applications like Sonarr. This is because of two factors:

1) Tubarr runs yt-dlp under the hood, which means Python and the Deno runtime must be included.

2) Tubarr runs transcoding operations (through Metarr), with support for various types of hardware acceleration and codecs. This requires the inclusion of a very fully-featured FFmpeg build and a large number of media codecs and drivers to reliably work on different machines. Most of the image size comes from this!

### Resources:

It is recommended you do not impose heavy resource limitations on the container itself. Tubarr/Metarr run heavy transcoding operations via FFmpeg so it is normal for CPU and RAM usage to spike during this process. Cutting the resources can dramatically slow down the speed of these operations.

Setting RAM and CPU limits is possible directly in a channel's settings (`--metarr-min-free-mem`, `--metarr-max-cpu`) and it is generally better to use this instead. These limits ensure the system has at least x amount of RAM remaining, or that CPU is operating below x%, before spawning a new transcoding operation.

---

## Quick Start

Since this is a Go (Golang) program you can just download the "tubarr" file and run it from terminal. If you put it in /usr/local/bin/ you can invoke it from terminal with the command "tubarr".

If you want the web frontend you should use the container, or clone the entire repository (with terminal command "git clone https://github.com/TubarrApp/Tubarr.git"). The web pages are read from the `web/` folder next to the `tubarr` binary, so keep the binary inside the repository folder.

To build manually from source:

```bash
git clone https://github.com/TubarrApp/Tubarr.git
cd Tubarr
go build -o tubarr ./cmd/tubarr
sudo ln -s "$PWD/tubarr" /usr/local/bin/tubarr
tubarr --help
```

(A symlink keeps the web frontend working, since Tubarr finds `web/` relative to the real binary. Moving the binary out of the repository with `mv` instead breaks the web frontend.)

Tubarr stores its database and logs under `~/.tubarr`. Metarr logs live in `~/.metarr`.

Only one Tubarr instance can run at a time. While the web server is running, other `tubarr` commands (including cron runs) will refuse to start.

---

## Adding a Channel

```bash
tubarr channel add \
  --channel-urls 'https://www.tubesite.com/@CoolChannel' \
  --channel-name 'Cool Channel' \
  --video-directory /home/user/Videos/{{channel_name}} \
  --json-directory /home/user/Videos/{{channel_name}}/meta \
  --metarr-meta-ops 'title:date-tag:prefix:ymd' \
  --metarr-meta-ops 'fulltitle:date-tag:prefix:ymd' \
  --metarr-output-directory /home/user/Videos/{{channel_name}}/{{year}} \
  --notify 'http://YOUR_PLEX_SERVER_IP:32400/library/sections/LIBRARY_ID_NUMBER/refresh?X-Plex-Token=YOUR_PLEX_TOKEN_HERE|Plex'
```

Repeat `--metarr-meta-ops` (and the other operation flags) once per operation.

Templates are resolved per channel or per video:

- **Any directory:** `{{channel_name}}`, `{{channel_id}}`, `{{channel_url}}`.
- **`--metarr-output-directory` only:** also `{{video_title}}`, `{{video_url}}`, `{{year}}`, `{{month}}`, `{{day}}`, `{{author}}`, `{{director}}`, `{{domain}}`. These are filled from each video's metadata by Metarr, so they're rejected in other directories.

Use `tubarr channel add-batch --add-from-directory ./configs` to import multiple channels from config files.

Pass `--auto-download=false` to list new videos under **Available** in the web UI instead of downloading them, and `--ignore-run` to skip a channel's existing backlog.

---

## Running the Crawler

- Default behavior (`tubarr`) crawls every channel that isn't paused and is due for a crawl (based on its `--crawl-freq`), then exits.
- Use `tubarr channel crawl --channel-name 'Cool Channel'` to manually trigger one channel.
- `tubarr channel download-video-urls --channel-name 'Cool Channel' --urls "channelURL|videoURL"` downloads ad-hoc videos without enabling the full crawler. A plain video URL (no `channelURL|`) downloads it to the channel's manual downloads.
- `tubarr channel run-metarr --channel-name 'Cool Channel'` runs Metarr on downloaded videos it failed on or skipped.

In web mode, Tubarr checks channels every few seconds and crawls each one when it's due, so cron isn't needed.

### Automation (cron)

For CLI-only use (not alongside the web server):

```
0 */2 * * * /usr/local/bin/tubarr > /dev/null 2>&1
```

(Tubarr already writes its own log to `~/.tubarr/tubarr.log`.)

---

## Web Interface

Start Tubarr as an HTTP server:

```bash
tubarr --web
```

Open `http://localhost:8827` to:

- Add/update/delete channels, and crawl, ignore-crawl, or stop them
- Watch active downloads and transcodes, and cancel them
- Pick videos to download from **Available**, or download URLs manually
- View logs (Tubarr's and Metarr's), blocked domains, site scraping rules, and settings

A channel's **Stop Queue** button stops it starting anything new, letting in-progress downloads and transcodes finish. **Abort All** stops everything for the channel immediately.

The web pages are served from the `web/` folder next to the `tubarr` binary. `GET /health` is available for container healthchecks.

### Logging in

The interface listens on `127.0.0.1:8827` by default (all addresses in Docker). Use `--listen 0.0.0.0:8827` (or `TUBARR_LISTEN`) to reach it from other machines.

- **First visit:** create a username and password. Tubarr asks for them on every new browser after that.
- **Skip login for trusted addresses:** under **Settings > Web Interface Access**, list IPs or ranges (e.g. `192.168.1.0/24`) that need no login. This machine (localhost) is on the list by default.
- **Scripts:** send the API key from the same page in an `X-Api-Key` header.
- **Forgotten password:** open Tubarr from the machine it runs on (localhost skips the login) and change it in Settings, or set `TUBARR_WEB_PASSWORD` (and optionally `TUBARR_WEB_USERNAME`) and restart.
- **Reverse proxy with its own login:** `--web-auth none` (or `TUBARR_WEB_AUTH=none`) turns Tubarr's login off. Add the proxy under **Trusted Proxies** so client addresses are read correctly.

---

## Docker / Podman

The repo includes:

- `Dockerfile`
- `examples/docker-compose.yaml` – recommended, uses the published `ghcr.io/tubarrapp/tubarr` image
- `examples/podman-quadlet.container` – Podman Quadlet equivalent
- `docker-compose.yml` – builds the image from your local checkout
- `DOCKER.md` (detailed guide)

Volumes:
| Host path             | Container | Purpose                |
|-----------------------|-----------|------------------------|
| e.g. `/opt/containers/tubarr/.tubarr` | `/home/tubarr/.tubarr` | DB, configs, logs      |
| e.g. `/opt/containers/tubarr/.metarr` | `/home/tubarr/.metarr` | Metarr logs and data   |
| e.g. `/home/user/videos` | `/videos` | Downloaded videos |

**Note:** Video and metadata directories are configured per-channel. Mount your desired host directories and reference them (by their container paths) when adding channels. Host folders must be owned by `PUID`:`PGID`.

Environment variables:
- `TZ=America/New_York` (change as needed)
- `PUID=1000` (user ID, change to match your host user)
- `PGID=1000` (group ID, change to match your host group)
- `UMASK=022` (permissions for new files and folders, e.g. `002` to let your group write; default `022`)

For hardware-accelerated transcoding, pass the GPU through with `devices: - /dev/dri:/dev/dri`.

**File permissions outside a container:** Tubarr and Metarr use the process umask, so set it where they're launched, e.g. `UMask=0002` in a systemd unit or `umask 002` in your shell.

---

## Configuration Highlights

- **Settings**: per-channel concurrency, crawl frequency, retry counts, `yt-dlp` args, cookie sources, date and metadata filters, templated directories, automatic or manual (Available) downloads.
- **Metarr args**: filename/meta operations, filtered operations, metadata-based move rules, FFmpeg transcode settings, per-URL output directories.
- **Notifications**: entries formatted as `NotifyURL|Friendly Name` or `ChannelURL|NotifyURL|Friendly Name`.
- **Global settings**: crawl concurrency, global and per-domain download limits, FlareSolverr URL, and custom site scraping rules (`--scrape-config-file`), also editable on the web UI's Settings page.
- **Security**: credentials are encrypted via `~/.tubarr/.passwords/aes.txt`.

---

## Logging & Troubleshooting

- Tubarr log: `~/.tubarr/tubarr.log`
- Metarr log: `~/.metarr/metarr.log` (Metarr also sends its logs to a running Tubarr, viewable on the web UI's Logs page)
- Run with `--debug 1` to `--debug 5` to increase verbosity.
- Check `docker compose logs -f tubarr` (or `podman logs tubarr`) when containerized.

---

## Contributing

1. Fork the repo, create a feature branch, and open a PR.
2. Run `go test ./...` plus linting defined in `.golangci.yaml`.
3. Document new CLI flags or config keys.

---

## License

See `LICENSE`.
