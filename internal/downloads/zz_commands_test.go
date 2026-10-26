package downloads

import (
	"slices"
	"testing"
	"tubarr/internal/models"
)

// TestCommandsLeaveRetriesToTubarr tests that Download Retries isn't passed to yt-dlp, as Tubarr runs the retries itself.
func TestCommandsLeaveRetriesToTubarr(t *testing.T) {
	vd, jd := newTestDownloads(t.Context(), t, "https://retries.example/v", 1)
	vd.ChannelURL.ChanURLSettings.Retries = 5
	jd.ChannelURL.ChanURLSettings.Retries = 5

	if args := vd.buildVideoCommand().Args; slices.Contains(args, "--retries") {
		t.Errorf("video command passes --retries to yt-dlp: %q", args)
	}
	if args := jd.buildJSONCommand().Args; slices.Contains(args, "--retries") {
		t.Errorf("JSON command passes --retries to yt-dlp: %q", args)
	}
}

// TestCommandsCombineCookies tests that a cookie file and a browser's cookies are both passed, for yt-dlp to combine.
func TestCommandsCombineCookies(t *testing.T) {
	vd, jd := newTestDownloads(t.Context(), t, "https://cookies.example/v", 1)
	for _, cu := range []*models.ChannelURL{vd.ChannelURL, jd.ChannelURL} {
		cu.CookiePath = "/tmp/cookies.txt"
		cu.ChanURLSettings.CookiesFromBrowser = "firefox"
	}

	for what, args := range map[string][]string{"video": vd.buildVideoCommand().Args, "JSON": jd.buildJSONCommand().Args} {
		if !slices.Contains(args, "--cookies") || !slices.Contains(args, "--cookies-from-browser") {
			t.Errorf("%s command doesn't pass both the cookie file and the browser: %q", what, args)
		}
	}
}
