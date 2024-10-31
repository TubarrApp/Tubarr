package domain

import (
	"Tubarr/internal/models"
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"os/exec"
	"strings"
)

type VideoCommandBuilder struct {
	Model *models.DownloadedFiles
}

func NewVideoCommandBuilder(d *models.DownloadedFiles) *VideoCommandBuilder {
	return &VideoCommandBuilder{
		Model: d,
	}
}

// BuildVideoFetchCommand builds the command for yt-dlp
func (vf *VideoCommandBuilder) VideoFetchCommand() (*exec.Cmd, error) {

	if vf.Model == nil {
		return nil, fmt.Errorf("model passed in null, returning no command")
	}

	vDir := vf.Model.VideoDirectory
	cookieSource := vf.Model.CookieSource
	url := vf.Model.URL
	eDl := vf.Model.ExternalDler
	eDlArgs := vf.Model.ExternalDlerArgs

	var args []string

	switch {
	case strings.Contains(url, "censored.tv"):
		// Not implemented
	default:
		// Use default
	}

	args = append(args, writeJsonLocation(vDir)...)
	args = append(args, "--restrict-filenames", "-o", "%(title)s.%(ext)s")
	args = append(args, "--retries", "999", "--retry-sleep", "10")
	args = append(args, "--print", "after_move:%(filepath)s")

	if len(cookieSource) > 0 {
		args = append(args, "--cookies-from-browser", cookieSource)
	}
	if len(eDl) > 0 {
		args = append(args, "--external-downloader", eDl)
	}
	if len(eDlArgs) > 0 {
		args = append(args, "--external-downloader-args", eDlArgs)
	}
	if len(url) != 0 {
		args = append(args, url)
	}

	logging.PrintD(1, "Built argument list: %v", args)

	return exec.Command("yt-dlp", args...), nil
}

func writeJsonLocation(s string) []string {
	if s != "" {
		return []string{"--write-info-json", "-P", s}
	}
	return nil
}
