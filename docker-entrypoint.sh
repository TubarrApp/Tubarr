#!/bin/sh

# Create group if needed
if ! getent group tubarr >/dev/null; then
    addgroup -g "$PGID" tubarr
fi

# Create user if needed
if ! id -u tubarr >/dev/null 2>&1; then
    adduser -D -u "$PUID" -G tubarr tubarr
fi

# Fix permissions on bind mounts
chown -R tubarr:tubarr /home/tubarr /downloads /metadata 2>/dev/null || true

# Run updater in background
/usr/local/bin/auto-updater &

# Drop to the correct user
exec su-exec tubarr /usr/local/bin/tubarr --web
