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

# Make home directory, chown necessary directories
mkdir -p /home/tubarr /downloads /metadata
chown -R "$PUID":"$PGID" /app/tubarr /downloads /metadata 2>/dev/null || true

# Run updater in background
/usr/local/bin/auto-updater &

# Start web interface
exec gosu "$USER" /app/tubarr --web
