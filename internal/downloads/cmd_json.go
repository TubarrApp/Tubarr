package downloads

import (
	"context"
	"os/exec"
	"strconv"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// RequestMetaCommand builds and returns the argument for downloading metadata files for the given URL.
func buildJSONCommand(v *models.Video) *exec.Cmd {

	args := make([]string, 0, 32)

	args = append(args,
		"--skip-download",
		"--write-info-json",
		"-P", v.JDir)

	if v.Settings.CookieSource != "" {
		args = append(args, "--cookies-from-browser", v.Settings.CookieSource)
	}

	if v.Settings.MaxFilesize != "" {
		args = append(args, "--max-filesize", v.Settings.MaxFilesize)
	}

	if v.Settings.ExternalDownloader != "" {
		args = append(args, "--external-downloader", v.Settings.ExternalDownloader)

		if v.Settings.ExternalDownloaderArgs != "" {
			args = append(args, "--external-downloader-args", v.Settings.ExternalDownloaderArgs)
		}
	}

	if v.Settings.Retries != 0 {
		args = append(args, "--retries", strconv.Itoa(v.Settings.Retries))
	}

	args = append(args, "--restrict-filenames", "-o", "%(title)s.%(ext)s",
		v.URL)

	cmd := exec.CommandContext(context.Background(), "yt-dlp", args...)
	logging.D(1, "Built metadata download command for URL %q:\n%v", v.URL, cmd.String())

	return cmd
}
