package command

import (
	"fmt"
	"os/exec"
	"strings"
	"tubarr/internal/cfg"
	keys "tubarr/internal/domain/keys"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

type VideoDLRequest struct {
	DL *models.DLs
}

func NewVideoDLRequest(dl *models.DLs) *VideoDLRequest {
	return &VideoDLRequest{
		DL: dl,
	}
}

// BuildVideoFetchCommand builds the command for yt-dlp
func (vf *VideoDLRequest) VideoFetchCommand() error {
	if vf.DL == nil {
		return fmt.Errorf("model passed in null, returning no command")
	}

	dl := vf.DL

	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return fmt.Errorf("yt-dlp command not found: %w", err)
	}

	if dl.URL == "" {
		return fmt.Errorf("url passed in blank")
	}

	var args []string
	switch {
	case strings.Contains(dl.URL, "censored.tv"):
		// Not implemented
	default:
		// Use default
	}

	args = append(args, "--restrict-filenames", "-o", "%(title)s.%(ext)s")

	if cfg.IsSet(keys.DLRetries) {
		args = append(args, "--retries", cfg.GetString(keys.DLRetries))
	}

	args = append(args, "--print", "after_move:%(filepath)s")

	if cfg.IsSet(keys.CookieSource) {
		args = append(args, "--cookies-from-browser", cfg.GetString(keys.CookieSource))
	}

	if cfg.IsSet(keys.ExternalDownloader) {
		args = append(args, "--external-downloader", cfg.GetString(keys.ExternalDownloader))
	}

	if cfg.IsSet(keys.ExternalDownloaderArgs) {
		args = append(args, "--external-downloader-args", cfg.GetString(keys.ExternalDownloaderArgs))
	}

	args = append(args, dl.URL)

	logging.D(1, "Built argument list: %v", args)
	dl.VideoCommand = exec.Command("yt-dlp", args...)
	logging.D(1, "Created download command for URL '%s'", dl.URL)

	return nil
}
