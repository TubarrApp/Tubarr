#!/bin/sh

PGID=${PGID:-1000}
PUID=${PUID:-1000}

if getent group "$PGID" >/dev/null 2>&1; then
    GROUP=$(getent group "$PGID" | cut -d: -f1)
else
    GROUP=tubarr
    groupadd -g "$PGID" "$GROUP"
fi

if getent passwd "$PUID" >/dev/null 2>&1; then
    USER=$(getent passwd "$PUID" | cut -d: -f1)
else
    USER=tubarr
    useradd -u "$PUID" -g "$PGID" -d /home/tubarr -s /bin/sh -m "$USER"
fi

mkdir -p /home/tubarr /downloads /metadata
chown -R "$PUID":"$PGID" /app/tubarr /downloads /metadata 2>/dev/null || true

# Run updater in background
/usr/local/bin/auto-updater &

exec gosu "$USER" /app/tubarr --web
