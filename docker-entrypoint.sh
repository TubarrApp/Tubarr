#!/bin/sh

# If a group with PGID exists, use it; otherwise create it
if getent group "$PGID" >/dev/null 2>&1; then
    GROUP_NAME=$(getent group "$PGID" | cut -d: -f1)
else
    GROUP_NAME=app
    groupadd -g "$PGID" "$GROUP_NAME"
fi

# If a user with PUID exists, use it; otherwise create it
if getent passwd "$PUID" >/dev/null 2>&1; then
    USER_NAME=$(getent passwd "$PUID" | cut -d: -f1)
else
    USER_NAME=app
    useradd -u "$PUID" -g "$PGID" -d /home/app -s /bin/sh -m "$USER_NAME"
fi

chown -R "$PUID":"$PGID" /home/app /downloads /metadata 2>/dev/null || true

exec gosu "$USER_NAME" /app/tubarr --web
