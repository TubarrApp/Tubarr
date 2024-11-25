package downloads

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

// buildVideoCommand builds the command to download a video using yt-dlp.
func buildVideoCommand(v *models.Video) *exec.Cmd {
	args := make([]string, 0, 64)

	args = append(args,
		"--restrict-filenames",
		"-o", filepath.Join(v.VDir, "%(title)s.%(ext)s"),
		"--retries", "infinite",
		"--fragment-retries", "infinite",
		"--extractor-retries", "infinite")

	args = append(args, "--print", "after_move:%(filepath)s")

	if v.Settings.CookieSource != "" {
		args = append(args, "--cookies-from-browser", v.Settings.CookieSource)
	}

	if v.Settings.MaxFilesize != "" {
		args = append(args, "--max-filesize", v.Settings.MaxFilesize)
	}

	if v.Settings.ExternalDownloaderArgs != "" {
		args = append(args, "--external-downloader", v.Settings.ExternalDownloader)
		dlArgs := v.Settings.ExternalDownloaderArgs

		// Add safety arguments if using aria2c
		if v.Settings.ExternalDownloader == "aria2c" {
			dlArgs = fmt.Sprintf("%s --retry-wait=10 --max-tries=5 --timeout=60 --connect-timeout=60 --min-tls-version=TLSv1.2", dlArgs)
		}

		args = append(args, "--external-downloader-args", dlArgs)
	}

	args = append(args, "--sleep-requests", "1", v.URL)

	logging.D(1, "Built argument list: %v", args)
	cmd := exec.Command("yt-dlp", args...)

	return cmd
}
