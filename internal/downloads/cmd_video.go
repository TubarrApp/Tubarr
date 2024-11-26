package downloads

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

// buildVideoCommand builds the command to download a video using yt-dlp.
func buildVideoCommand(v *models.Video) *exec.Cmd {
	args := make([]string, 0, 64)

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
			args = append(args, "--external-downloader-args", v.Settings.ExternalDownloaderArgs)
		}
	}

	if v.Settings.Retries != 0 {
		args = append(args, "--retries", strconv.Itoa(v.Settings.Retries))
	}

	args = append(args, "--sleep-requests", "1", v.URL)

	logging.D(1, "Built argument list: %v", args)
	cmd := exec.Command("yt-dlp", args...)

	return cmd
}
