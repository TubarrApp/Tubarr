#!/bin/sh

PGID=${PGID:-1000}
PUID=${PUID:-1000}

# Give the image's tubarr user and group the requested IDs, so files Tubarr writes belong to the host user (-o allows
# an ID another user or group already has). usermod also re-owns the files in the home directory.
if [ "$(getent group tubarr | cut -d: -f3)" != "$PGID" ]; then
    groupmod -o -g "$PGID" tubarr
fi
if [ "$(id -u tubarr)" != "$PUID" ] || [ "$(id -g tubarr)" != "$PGID" ]; then
    usermod -o -u "$PUID" -g "$PGID" tubarr
fi
chown "$PUID":"$PGID" /home/tubarr /home/tubarr/.tubarr 2>/dev/null || true

# Required for hardware acceleration
usermod -aG video tubarr 2>/dev/null || true

# Fix /tmp permissions
chmod 1777 /tmp

# Make and chown the download directories (the Tubarr binary stays root-owned, so Tubarr can't replace it)
mkdir -p /downloads /metadata
chown -R "$PUID":"$PGID" /downloads /metadata 2>/dev/null || true

# File creation mask, inherited by the updater, Tubarr, and its child processes (yt-dlp, Metarr, ffmpeg)
umask "${UMASK:-022}"

# Run updater in background, as root with root's own home, so pip and Python don't read config the tubarr user can write
HOME=/root PYTHONNOUSERSITE=1 /usr/local/bin/auto-updater &

# Start Tubarr with the container's arguments, or the web interface if none are given
[ $# -eq 0 ] && set -- --web
exec gosu tubarr /app/tubarr "$@"
