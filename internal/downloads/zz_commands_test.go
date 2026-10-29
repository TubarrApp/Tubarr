package downloads

import (
	"os/exec"
	"slices"
	"testing"
	"tubarr/internal/domain/command"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// TestCommandsLeaveRetriesToTubarr tests that Download Retries isn't passed to yt-dlp, as Tubarr runs the retries itself.
func TestCommandsLeaveRetriesToTubarr(t *testing.T) {
	vd, jd := newTestDownloads(t.Context(), t, "https://retries.example/v", 1)
	vd.ChannelURL.ChanURLSettings.Retries = 5
	jd.ChannelURL.ChanURLSettings.Retries = 5

	if args, _ := vd.buildVideoCommand(); slices.Contains(args.Args, "--retries") {
		t.Errorf("video command passes --retries to yt-dlp: %q", args)
	}
	if args, _ := jd.buildJSONCommand(); slices.Contains(args.Args, "--retries") {
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

	for what, build := range map[string]func() (*exec.Cmd, error){
		"video": vd.buildVideoCommand,
		"JSON":  jd.buildJSONCommand,
	} {
		cmd, err := build()
		if err != nil {
			t.Fatalf("building %s command: %v", what, err)
		}

		args := cmd.Args
		if !slices.Contains(args, "--cookies") || !slices.Contains(args, "--cookies-from-browser") {
			t.Errorf("%s command doesn't pass both the cookie file and the browser: %q", what, args)
		}
	}
}

// TestVideoCommandExtSame tests an extension is passed to yt-dlp as its merge output format, and 'same' passes none.
func TestVideoCommandExtSame(t *testing.T) {
	vd, _ := newTestDownloads(t.Context(), t, "https://ext.example/v", 1)

	// An extension.
	vd.ChannelURL.ChanURLSettings.YtdlpOutputExt = "mkv"
	if args, _ := vd.buildVideoCommand(); !slices.Contains(args.Args, command.YtDLPOutputExtension) {
		t.Errorf("mkv not passed to yt-dlp: %q", args)
	}

	// Same as input.
	vd.ChannelURL.ChanURLSettings.YtdlpOutputExt = sharedconsts.ExtSame
	if args, _ := vd.buildVideoCommand(); slices.Contains(args.Args, command.YtDLPOutputExtension) {
		t.Errorf("%q passed a merge output format to yt-dlp: %q", sharedconsts.ExtSame, args)
	}
}
