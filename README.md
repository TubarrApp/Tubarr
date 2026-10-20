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

For a login on every URL, use `--auth-username`, `--auth-password` and `--auth-url`. For a login on one URL, repeat `--auth-details` with JSON: `--auth-details '{"channel_url":"https://www.tubesite.com/@CoolChannel","username":"me","password":"secret","login_url":"https://www.tubesite.com/login"}'`.

`--download-concurrency` sets how many videos a channel downloads at once (`download-concurrency` in config files).

Templates are resolved per channel or per video:

- **Any directory:** `{{channel_name}}`, `{{channel_id}}`, `{{channel_url}}`.
- **`--metarr-output-directory` only:** also `{{video_title}}`, `{{video_url}}`, `{{year}}`, `{{month}}`, `{{day}}`, `{{author}}`, `{{director}}`, `{{domain}}`. These are filled from each video's metadata by Metarr, so they're rejected in other directories.

Date tags take an optional format after a colon:

| Tag | Example | Formats |
|---|---|---|
| `{{year}}` | `2026` | `{{year:yyyy}}` → `2026` (default), `{{year:yy}}` → `26` |
| `{{month}}` | `01` | `{{month:number}}` → `01` (default), `{{month:abbreviated}}` → `Jan`, `{{month:word}}` → `January` |
| `{{day}}` | `07` | none |

For example, `--metarr-output-directory '/media/{{channel_name}}/{{year}}/{{month:abbreviated}}'` sorts videos into folders like `/media/Cats/2026/Jan`.

Use `tubarr channel add-batch --add-from-directory ./configs` to import multiple channels from config files.

New videos wait under **Available** (in the web UI, or `tubarr channel list-available`) for you to choose from. Pass `--auto-download` to download them automatically instead, and `--ignore-run` to skip a channel's existing backlog.

### File names

Videos download as `Title [video ID]`, so videos with the same title, or a title in a non-Latin script, don't overwrite each other. When Metarr runs, it renames each finished video to its full title, keeping the extension and numbering a name that's already taken. To name files another way, give the channel its own `set` filename operation, e.g. `--metarr-filename-ops 'set:{{artist}} - {{fulltitle}}'`. Without Metarr, files keep their download names.

`--metarr-purge-metafile` (`all`, `json` or `nfo`) makes Metarr delete a video's metadata files once it has processed it.

---

## Running the Crawler

- Default behavior (`tubarr`) crawls every channel that isn't paused and is due for a crawl, then exits. Each channel URL is due on its own crawl frequency if it has one, or the channel's `--crawl-freq`, and a crawl only checks the URLs that are due.
- Use `tubarr channel crawl --channel-name 'Cool Channel'` to manually trigger one channel.
- `tubarr channel download-video-urls --channel-name 'Cool Channel' --urls videoURL` downloads ad-hoc videos without enabling the full crawler, to the channel's manual downloads. Add `--channel-url` with one of the channel's URLs to download them through it (e.g. for its authentication), or `--send-to-available` to add them to **Available** instead of downloading them.
- `tubarr channel run-metarr --channel-name 'Cool Channel'` runs Metarr on downloaded videos it failed on or skipped.
- `tubarr channel list-available` lists videos waiting in **Available** with their IDs (add `--channel-name` for one channel). `tubarr channel download-available --channel-name 'Cool Channel'` lists the channel's videos numbered and asks which to download (e.g. `1,3,5-8` or `all`), or pass `--video-ids`, `--urls` or `--all` to skip the prompt. `dismiss-available` works the same way to dismiss them.
- `tubarr channel delete-videos --channel-name 'Cool Channel' --urls videoURL` removes videos from the channel's list, so later crawls treat them as new. Their files are kept unless you add `--delete-files`.

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

The interface listens on `127.0.0.1:8827` by default (all addresses in Docker). Use `--web-address 0.0.0.0:8827` (or `TUBARR_WEB_ADDRESS`) to reach it from other machines. The port defaults to 8827 if left out.

- **First visit:** create a username and password. Tubarr asks for them on every new browser after that.
- **Login mode:** under **Settings > Web Interface Access**, choose **Required for everyone**, **Required, except from localhost** (the default, so only a browser on the machine Tubarr runs on skips it) or **Off**. `--web-auth required|local|none` (or `TUBARR_WEB_AUTH`) overrides it.
- **Skip login for trusted addresses:** on the same page, list IPs or ranges (e.g. `192.168.1.0/24`) that need no login.
- **Scripts:** send the API key from the same page in an `X-Api-Key` header.
- **Forgotten password:** run `tubarr auth reset` (it works while Tubarr is running) and create a new login on the next visit. Or open Tubarr from the machine it runs on, if the login mode skips it there, and change it in Settings, or set `TUBARR_WEB_PASSWORD` and restart. The saved username is kept unless `TUBARR_WEB_USERNAME` is also set, and it must be when no login exists yet. While these are set they're applied at every start, replacing a login changed in Settings, but browsers stay signed in if nothing changed. In Docker, requests from the host come from the Docker network, so the localhost login skip doesn't apply.
- **Reverse proxy:** add the proxy's address under **Trusted Proxies** (e.g. `127.0.0.1` for one on the same machine), and have it send `X-Forwarded-For`, so Tubarr sees each visitor's real address. Requests relayed by a proxy that isn't trusted always need the login. A proxy that sends no forwarding headers at all (e.g. nginx with only `proxy_pass`) can't be told apart from a browser on this machine, so add `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;` or set the login mode to **Required for everyone**.
- **Reverse proxy with its own login:** set the login mode to **Off** (or `--web-auth none`, `TUBARR_WEB_AUTH=none`). Add the proxy under **Trusted Proxies** so client addresses are read correctly. Without a login, Tubarr only answers its own hostnames and IP addresses, so add the proxy's hostname under **Allowed Hostnames**, or in `TUBARR_WEB_ALLOWED_HOSTS` (comma-separated, e.g. `tubarr.example.com`).

### HTTPS

Tick **Use HTTPS** under **Settings > HTTPS** and restart Tubarr to serve the interface at `https://localhost:8827`. `--web-https` (or `TUBARR_WEB_HTTPS=true`/`false`) overrides the setting.

- **Default:** a self-signed certificate, saved as `tls-self-signed.pem` in the Tubarr folder and reused (including across container updates), so the browser warning only needs accepting once. It's renewed automatically a month before it expires; delete the file and restart to make a new one sooner.
- **Your own certificate:** set the path under **Settings > HTTPS**, or with `--web-tls-cert` / `TUBARR_WEB_TLS_CERT` (and `--web-tls-key` / `TUBARR_WEB_TLS_KEY` if the key is in a separate file). A PEM file holding both the certificate and key works on its own. Renewed files are picked up without a restart.
- **Reverse proxy:** leave HTTPS off and let the proxy (Caddy, Traefik, nginx) handle certificates, or point the proxy at `https://` and skip verification of the self-signed certificate.

---

## Docker / Podman

The repo includes:

- `Dockerfile`
- `examples/docker-compose.yaml` – uses the published `ghcr.io/tubarrapp/tubarr` image
- `examples/podman-quadlet.container` – Podman Quadlet equivalent
- `DOCKER.md` (detailed guide, including building the image from a local checkout)

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

- **Settings**: per-channel download concurrency, crawl frequency, retry counts, `yt-dlp` args, cookie sources, date and metadata filters, templated directories, automatic or manual (Available) downloads. A channel URL's own settings override the channel's, and any it leaves blank use the channel's.
- **Metarr args**: filename/meta operations, filtered operations, metadata-based move rules, FFmpeg transcode settings, metadata file deletion, per-URL output directories.
- **Notifications**: entries formatted as `NotifyURL`, `NotifyURL|Friendly Name` or `ChannelURL|NotifyURL|Friendly Name`, one per line in the web UI.
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
