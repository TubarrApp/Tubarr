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
		cu.Session.CookiePath = "/tmp/cookies.txt"
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

// TestVideoCommandNamedAfterJSON tests the video is named after its JSON file, so a title changed since can't make them differ.
func TestVideoCommandNamedAfterJSON(t *testing.T) {
	tests := []struct {
		name       string
		jsonFile   string
		customFile string
		want       string
	}{
		{"no JSON", "", "", command.FilenameSyntax},
		{"yt-dlp JSON", "/json/Old_Title [abc123].info.json", "", "Old_Title [abc123].%(ext)s"},
		{"custom JSON wins", "/json/Old_Title [abc123].info.json", "/json/Scraped [1a2b3c4d].json", "Scraped [1a2b3c4d].%(ext)s"},
		{"percent escaped", "/json/100% [abc123].info.json", "", "100%% [abc123].%(ext)s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vd, _ := newTestDownloads(t.Context(), t, "https://name.example/v", 1)
			vd.Video.Files.JSONFilePath = tt.jsonFile
			vd.Video.Files.JSONCustomFile = tt.customFile

			cmd, err := vd.buildVideoCommand()
			if err != nil {
				t.Fatal(err)
			}
			i := slices.Index(cmd.Args, command.Output)
			if i < 0 || i+1 >= len(cmd.Args) {
				t.Fatalf("no %s in video command: %q", command.Output, cmd.Args)
			}
			if got := cmd.Args[i+1]; got != tt.want {
				t.Errorf("output template %q, want %q", got, tt.want)
			}
		})
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
