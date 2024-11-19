package command

import (
	"fmt"
	"os/exec"
	"strconv"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

type MetaDLRequest struct {
	Videos *models.Video
}

func NewMetaDLRequest(videos *models.Video) *MetaDLRequest {
	return &MetaDLRequest{
		Videos: videos,
	}
}

// RequestMetaCommand builds and returns the argument for downloading metadata
// files for the given URL
func (mdl *MetaDLRequest) RequestMetaCommand() *exec.Cmd {
	v := mdl.Videos
	var buildArgs []string

	buildArgs = append(buildArgs,
		"--skip-download",
		"--write-info-json",
		"-P", v.JDir,
		"--retries", "5", // Number of retries for the entire download
		"--fragment-retries", "10", // Number of retries for each fragment
		"--socket-timeout", "30", // Socket timeout in seconds
		"--extractor-retries", "3") // Retries for extractor

	if v.Settings.CookieSource != "" {
		buildArgs = append(buildArgs, "--cookies-from-browser", v.Settings.CookieSource)
	}

	if v.Settings.Retries != 0 {
		buildArgs = append(buildArgs, "--retries", strconv.Itoa(v.Settings.Retries))
	}

	if v.Settings.ExternalDownloader != "" {
		buildArgs = append(buildArgs, "--external-downloader", v.Settings.ExternalDownloader)
	}

	if v.Settings.ExternalDownloaderArgs != "" {
		dlArgs := v.Settings.ExternalDownloaderArgs
		// Add some safety args for aria2c
		if v.Settings.ExternalDownloader == "aria2c" {
			dlArgs = fmt.Sprintf("%s --retry-wait=10 --max-tries=5 --timeout=60 --connect-timeout=60", dlArgs)
		}
		buildArgs = append(buildArgs, "--external-downloader-args", dlArgs)
	}

	buildArgs = append(buildArgs, "--restrict-filenames", "-o", "%(title)s.%(ext)s",
		v.URL)

	cmd := exec.Command("yt-dlp", buildArgs...)
	logging.D(3, "Built metadata command for URL %q:\n%v", v.URL, cmd.String())

	return cmd
}
