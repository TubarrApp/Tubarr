package downloads

import (
	"os/exec"
	"strconv"
	"strings"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

// RequestMetaCommand builds and returns the argument for downloading metadata files for the given URL.
func buildJSONCommand(v *models.Video) *exec.Cmd {

	args := make([]string, 0, 64)

	args = append(args,
		"--skip-download",
		"--write-info-json",
		"-P", v.JDir,
		"--retries", "5", // Number of retries for the entire download
		"--fragment-retries", "10", // Number of retries for each fragment
		"--socket-timeout", "30", // Socket timeout in seconds
		"--extractor-retries", "3") // Retries for extractor

	if v.Settings.CookieSource != "" {
		args = append(args, "--cookies-from-browser", v.Settings.CookieSource)
	}

	if v.Settings.Retries != 0 {
		args = append(args, "--retries", strconv.Itoa(v.Settings.Retries))
	}

	if v.Settings.ExternalDownloader != "" {
		args = append(args, "--external-downloader", v.Settings.ExternalDownloader)
	}

	if v.Settings.MaxFilesize != "" {
		args = append(args, "--max-filesize", v.Settings.MaxFilesize)
	}

	if v.Settings.ExternalDownloader != "" {
		args = append(args, "--external-downloader", v.Settings.ExternalDownloader)

		// Build aria2c args as a single string
		if v.Settings.ExternalDownloader == "aria2c" {
			ariaArgs := []string{
				"retry-wait=3",
				"max-tries=0",
				"max-file-not-found=5",
				"check-certificate=false",
				"min-tls-version=TLSv1.2",
				"min-split-size=1M",
				"max-connection-per-server=16",
				"stream-piece-selector=inorder",
				"allow-piece-length-change=true",
				"auto-file-renaming=false",
				"continue=true",
			}

			// Join with -- prefix and combine with user args
			ariaArgsStr := "--" + strings.Join(ariaArgs, " --")
			if v.Settings.ExternalDownloaderArgs != "" {
				ariaArgsStr = ariaArgsStr + " " + v.Settings.ExternalDownloaderArgs
			}

			args = append(args, "--external-downloader-args", ariaArgsStr)
		} else if v.Settings.ExternalDownloaderArgs != "" {
			args = append(args, "--external-downloader-args", v.Settings.ExternalDownloaderArgs)
		}
	}

	args = append(args, "--restrict-filenames", "-o", "%(title)s.%(ext)s",
		v.URL)

	cmd := exec.Command("yt-dlp", args...)
	logging.D(3, "Built metadata command for URL %q:\n%v", v.URL, cmd.String())

	return cmd
}
