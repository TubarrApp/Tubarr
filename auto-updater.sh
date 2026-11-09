#!/bin/sh
# Updates yt-dlp and curl_cffi together at container start, then every 6 hours, once Tubarr is idle. Runs as root (started before gosu).
#
# The curl-cffi extra installs the newest stable curl_cffi in yt-dlp's supported range, then yt-dlp alone is moved
# to its nightly build ('--pre' on the first install would take beta dependencies too). 'yt-dlp -U' can't update a
# pip install, so pip is used.

pip_install() {
    pip3 install -q --root-user-action=ignore --no-cache-dir --break-system-packages "$@"
}

# Waits while Tubarr reports running jobs (503); no web server (terminal mode or still starting) means nothing to wait for.
# Tubarr writes its URL to the temp file at startup. Certificates aren't checked, as most are self-signed.
wait_for_idle() {
    while true; do
        url=$(cat "${TMPDIR:-/tmp}/tubarr-web-url" 2>/dev/null) || return
        wget -q -O /dev/null --no-check-certificate "$url/api/v1/status/idle"
        [ $? -eq 8 ] || return
        sleep 30
    done
}

while true; do
    wait_for_idle
    { pip_install -U "yt-dlp[default,curl-cffi]" && pip_install -U --pre --no-deps yt-dlp; } \
        || echo "auto-updater: failed to update yt-dlp/curl_cffi" >&2
    sleep 21600
done
