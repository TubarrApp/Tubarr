#!/bin/sh
# Updates yt-dlp and curl_cffi together at container start, then daily. Runs as root (started before gosu).
#
# The curl-cffi extra installs the newest stable curl_cffi in yt-dlp's supported range, then yt-dlp alone is moved
# to its nightly build ('--pre' on the first install would take beta dependencies too). 'yt-dlp -U' can't update a
# pip install, so pip is used.

pip_install() {
    pip3 install -q --root-user-action=ignore --no-cache-dir --break-system-packages "$@"
}

while true; do
    { pip_install -U "yt-dlp[default,curl-cffi]" && pip_install -U --pre --no-deps yt-dlp; } \
        || echo "auto-updater: failed to update yt-dlp/curl_cffi" >&2
    sleep 86400
done
