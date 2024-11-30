package downloads

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

const (
	ariaBase = len(consts.DLerAria) + len(": --console-log-level=info")
)

// buildVideoCommand builds the command to download a video using yt-dlp.
func buildVideoCommand(v *models.Video) *exec.Cmd {
	args := make([]string, 0, 32)

	args = append(args,
		"--restrict-filenames",
		"-o", filepath.Join(v.VDir, "%(title)s.%(ext)s"))

	args = append(args, "--print", "after_move:%(filepath)s")

	if v.Settings.CookieSource != "" {
		args = append(args, "--cookies-from-browser", v.Settings.CookieSource)
	}

	if v.Settings.MaxFilesize != "" {
		args = append(args, "--max-filesize", v.Settings.MaxFilesize)
	}

	if v.Settings.ExternalDownloader != "" {
		args = append(args, "--external-downloader", v.Settings.ExternalDownloader)
		if v.Settings.ExternalDownloaderArgs != "" {

			switch v.Settings.ExternalDownloader {
			case consts.DLerAria:
				var b strings.Builder

				b.Grow(ariaBase + len(v.Settings.ExternalDownloaderArgs))
				b.WriteString(consts.DLerAria)
				b.WriteRune(':')
				b.WriteString(v.Settings.ExternalDownloaderArgs) // "aria2c:-x 16 -s 16 --console-log-level=info"
				b.WriteString(" --console-log-level=info")

				args = append(args, "--external-downloader-args", b.String())
			default:
				args = append(args, "--external-downloader-args", v.Settings.ExternalDownloaderArgs)
			}
		}
	}

	if v.Settings.Retries != 0 {
		args = append(args, "--retries", strconv.Itoa(v.Settings.Retries))
	}

	args = append(args, "--sleep-requests", "1", v.URL)

	cmd := exec.CommandContext(context.Background(), "yt-dlp", args...)
	logging.D(1, "Built video download command for URL %q:\n%v", v.URL, cmd.String())

	return cmd
}
