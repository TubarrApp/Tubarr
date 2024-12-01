package downloads

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/videocmd"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

const (
	ariaBase = len(consts.DLerAria) + len(": ") + len(videocmd.AriaLog)
)

// buildVideoCommand builds the command to download a video using yt-dlp.
func buildVideoCommand(v *models.Video) *exec.Cmd {
	args := make([]string, 0, 32)

	args = append(args,
		videocmd.RestrictFilenames,
		videocmd.Output, filepath.Join(v.VDir, videocmd.FilenameSyntax))

	args = append(args, videocmd.Print, videocmd.AfterMove)

	if v.Settings.CookieSource != "" {
		args = append(args, videocmd.CookieSource, v.Settings.CookieSource)
	}

	if v.Settings.MaxFilesize != "" {
		args = append(args, videocmd.MaxFilesize, v.Settings.MaxFilesize)
	}

	if v.Settings.ExternalDownloader != "" {
		args = append(args, videocmd.ExternalDLer, v.Settings.ExternalDownloader)
		if v.Settings.ExternalDownloaderArgs != "" {

			switch v.Settings.ExternalDownloader {
			case consts.DLerAria:
				var b strings.Builder

				b.Grow(ariaBase + len(v.Settings.ExternalDownloaderArgs))
				b.WriteString(consts.DLerAria)
				b.WriteRune(':')
				b.WriteString(v.Settings.ExternalDownloaderArgs) // "aria2c:-x 16 -s 16 --console-log-level=info"
				b.WriteRune(' ')
				b.WriteString(videocmd.AriaLog)

				args = append(args, videocmd.ExternalDLArgs, b.String())
			default:
				args = append(args, videocmd.ExternalDLArgs, v.Settings.ExternalDownloaderArgs)
			}
		}
	}

	if v.Settings.Retries != 0 {
		args = append(args, videocmd.Retries, strconv.Itoa(v.Settings.Retries))
	}

	args = append(args, videocmd.SleepRequests, videocmd.SleepRequestsNum, v.URL)

	cmd := exec.CommandContext(context.Background(), videocmd.YTDLP, args...)
	logging.D(1, "Built video download command for URL %q:\n%v", v.URL, cmd.String())

	return cmd
}
