# Tubarr

Tubarr is a Go-powered companion to Metarr that keeps long-form video libraries fresh. It crawls creator channels, downloads the episodes with `yt-dlp`, applies your naming/metadata rules, and can refresh Plex/Emby/Jellyfin libraries automatically—all from the CLI or an optional web UI.

---

## Features

- **Channel-aware crawler** – schedule scans per channel, respect pauses, and avoid bot detection with randomized wait windows.
- **Config-driven downloads** – store channel settings in Viper-compatible files (YAML/TOML/JSON) and batch-import them.
- **Metarr integration** – share templated directories, filename/meta operations, output routing, and run the Metarr CLI automatically when available.
- **Web dashboard** – start Tubarr in web mode (`tubarr --web`) for a UI that manages channels, logs, downloads, and notifications.
- **Notifications & hooks** – call arbitrary URLs (e.g., Plex library refresh) when new videos land.
- **Docker-friendly** – ship with Dockerfile, Compose file, and Podman notes for unattended deployments.

---

## Requirements

- `yt-dlp` on `PATH`
- Optional: `metarr` for tagging, outputting to directories, etc.

(All included in the container image)

---

## Container Note

The Tubarr container image is a little over a gigabyte, making it larger than images of similar applications like Sonarr. This is because of two factors:

1) Tubarr runs yt-dlp under the hood, which means Python and the Deno runtime must be included.

2) Tubarr runs transcoding operations, with support for various types of hardware acceleration and codecs. This requires the inclusion of a very fully-featured FFmpeg build and a large number of media codecs and drivers to reliably work on different machines. Most of the image size comes from this!

### Resources:

It is recommended you do not impose heavy resource limitations on the container itself. Tubarr/Metarr run heavy transcoding operations via FFmpeg so it is normal for CPU and RAM usage to spike during this process. Cutting the resources can dramatically slow down the speed of these operations.

Setting RAM and CPU limits is possible directly in a channel's settings and it is generally better to use this instead. These limits ensure the system has at least x amount of RAM remaining, or that CPU is operating below x%, before spawning a new transcoding operation.

---

## Quick Start

```bash
git clone https://github.com/TubarrApp/Tubarr.git
cd Tubarr
go build -o tubarr ./cmd/tubarr
sudo mv tubarr /usr/local/bin/tubarr
tubarr --help
```

Tubarr stores its database and logs under `~/.tubarr`. Metarr logs live in `~/.metarr`.

---

## Adding a Channel

```bash
tubarr channel add \
  --channel-urls 'https://www.tubesite.com/@CoolChannel' \
  --channel-name 'Cool Channel' \
  --video-directory /home/user/Videos/{{channel_name}} \
  --json-directory /home/user/Videos/{{channel_name}}/meta \
  --metarr-meta-ops 'title:date-tag:prefix:ymd','fulltitle:date-tag:prefix:ymd' \
  --metarr-output-directory /home/user/Videos/{{channel_name}}/{{year}} \
  --notify 'http://YOUR_PLEX_SERVER_IP:32400/library/sections/LIBRARY_ID_NUMBER/refresh?X-Plex-Token=YOUR_PLEX_TOKEN_HERE|Plex'
```

Templates such as `{{channel_name}}`, `{{year}}`, and Metarr fields (e.g., `{{author}}`) are resolved per video. Use `tubarr channel add-batch --add-from-directory ./configs` to import multiple channels from config files.

---

## Running the Crawler

- Default behavior (`tubarr`) crawls every channel that isn’t paused and runs pending downloads.
- Use `tubarr channel crawl --channel-name 'Cool Channel'` to manually trigger one channel.
- `tubarr channel download-video-urls --channel-name 'Cool Channel' --urls "channelURL|videoURL"` downloads ad-hoc videos without enabling the full crawler.

### Automation (cron)

```
0 */2 * * * /usr/local/bin/tubarr >> ~/.tubarr/tubarr.log 2>&1
```

---

## Web Interface

Start Tubarr as an HTTP server:

```bash
tubarr --web
```

Open `http://localhost:8827` to:

- View/download logs
- Add/update/delete channels
- Inspect downloads and notifications

Static assets are bundled under `web/` and served from the compiled binary.

---

## Docker / Podman

The repo includes:

- `Dockerfile`
- `docker-compose.yml`
- `DOCKER.md` (detailed guide)

Volumes:
| Host path             | Container | Purpose                |
|-----------------------|-----------|------------------------|
| `./tubarr`     | `/home/tubarr/.tubarr` | DB, configs, logs      |

**Note:** Video and metadata directories are configured per-channel. Mount your desired host directories and reference them when adding channels.

Environment variables:
- `TZ=America/New_York` (change as needed)
- `PUID=1000` (user ID, change to match your host user)
- `PGID=1000` (group ID, change to match your host group)

---

## Configuration Highlights

- **Settings**: per-channel concurrency, retry counts, `yt-dlp` args, cookie sources, date filters, templated directories.
- **Metarr args**: filename/meta operations, filtered move rules, FFmpeg transcode settings, per-URL output directories.
- **Notifications**: entries formatted as `NotifyURL|Friendly Name` or `ChannelURL|NotifyURL|Friendly Name`.
- **Security**: credentials are encrypted via `~/.tubarr/.passwords/aes.txt`.

---

## Logging & Troubleshooting

- Tubarr log: `~/.tubarr/tubarr.log`
- Metarr log: `~/.metarr/metarr.log`
- Run with `--debug` flags to increase verbosity.
- Check `docker-compose logs -f` (or `podman logs`) when containerized.

---

## Contributing

1. Fork the repo, create a feature branch, and open a PR.
2. Run `go test ./...` plus linting defined in `.golangci.yaml`.
3. Document new CLI flags or config keys.

---

## License

See `LICENSE` (if present) or repository details for the definitive terms.
