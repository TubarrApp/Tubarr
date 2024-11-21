package command

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"tubarr/internal/cfg"
	keys "tubarr/internal/domain/keys"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

type VideoDLRequest struct {
	Video *models.Video
}

func NewVideoDLRequest(v *models.Video) *VideoDLRequest {
	return &VideoDLRequest{
		Video: v,
	}
}

// BuildVideoFetchCommand builds the command for yt-dlp
func (vf *VideoDLRequest) VideoFetchCommand() (*exec.Cmd, error) {
	if vf.Video == nil {
		return nil, fmt.Errorf("model passed in null, returning no command")
	}

	if vf.Video.VDir == "" {
		return nil, fmt.Errorf("output directory entered blank")
	}

	v := vf.Video

	if _, err := exec.LookPath("yt-dlp"); err != nil {
		logging.E(0, "yt-dlp command not found: %v", err)
		os.Exit(1)
	}

	if v.URL == "" {
		return nil, fmt.Errorf("url passed in blank")
	}

	var args []string

	args = append(args, "--restrict-filenames",
		"-o", filepath.Join(v.VDir, "%(title)s.%(ext)s"),
		"--retries", "5",
		"--fragment-retries", "10",
		"--socket-timeout", "30",
		"--extractor-retries", "3")

	if v.Settings.Retries > 0 {
		args = append(args, "--retries", cfg.GetString(keys.DLRetries))
	}

	args = append(args, "--print", "after_move:%(filepath)s")

	if v.Settings.CookieSource != "" {
		args = append(args, "--cookies-from-browser", v.Settings.CookieSource)
	}

	if v.Settings.ExternalDownloader != "" {
		args = append(args, "--external-downloader", v.Settings.ExternalDownloader)

		if v.Settings.ExternalDownloaderArgs != "" {
			dlArgs := v.Settings.ExternalDownloaderArgs
			// Add safety args for aria2c
			if v.Settings.ExternalDownloader == "aria2c" {
				dlArgs = fmt.Sprintf("%s --retry-wait=10 --max-tries=5 --timeout=60 --connect-timeout=60", dlArgs)
			}
			args = append(args, "--external-downloader-args", dlArgs)
		}
	}

	args = append(args, "--sleep-requests", "1",
		v.URL) // Add delay between requests

	logging.D(1, "Built argument list: %v", args)
	cmd := exec.Command("yt-dlp", args...)

	logging.D(1, "Created download command for URL %q, command: %s", v.URL, cmd)

	return cmd, nil
}
