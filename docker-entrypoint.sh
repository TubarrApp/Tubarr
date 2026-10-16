#!/bin/sh

PGID=${PGID:-1000}
PUID=${PUID:-1000}

# If user with GID $PGID exists, use it, else make default tubarr group
if getent group "$PGID" >/dev/null 2>&1; then
    GROUP=$(getent group "$PGID" | cut -d: -f1)
else
    GROUP=tubarr
    groupadd -g "$PGID" "$GROUP"
fi

# If user with UID $PUID exists, use it, else make default tubarr user
if getent passwd "$PUID" >/dev/null 2>&1; then
    USER=$(getent passwd "$PUID" | cut -d: -f1)
else
    USER=tubarr
    useradd -u "$PUID" -g "$PGID" -d /home/tubarr -s /bin/sh -m "$USER"
fi

# Required for hardware acceleration
usermod -aG video "$USER" 2>/dev/null || true

# Fix /tmp permissions
chmod 1777 /tmp

# Make home directory, chown necessary directories (the Tubarr binary stays root-owned, so Tubarr can't replace it)
mkdir -p /home/tubarr /downloads /metadata
chown -R "$PUID":"$PGID" /downloads /metadata 2>/dev/null || true

# File creation mask, inherited by the updater, Tubarr, and its child processes (yt-dlp, Metarr, ffmpeg)
umask "${UMASK:-022}"

# Run updater in background, as root with root's own home, so pip and Python don't read config the tubarr user can write
HOME=/root PYTHONNOUSERSITE=1 /usr/local/bin/auto-updater &

# Start web interface
exec gosu "$USER" /app/tubarr --web
