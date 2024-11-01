package command

import (
	"Tubarr/internal/models"
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"os/exec"
	"strings"
)

type VideoDLCommandBuilder struct {
	Model *models.DownloadedFiles
}

func NewVideoDLCommandBuilder(d *models.DownloadedFiles) *VideoDLCommandBuilder {
	return &VideoDLCommandBuilder{
		Model: d,
	}
}

// BuildVideoFetchCommand builds the command for yt-dlp
func (vf *VideoDLCommandBuilder) VideoFetchCommand() (*exec.Cmd, error) {
	if vf.Model == nil {
		return nil, fmt.Errorf("model passed in null, returning no command")
	}
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return nil, fmt.Errorf("yt-dlp command not found: %w", err)
	}

	m := vf.Model
	var args []string

	switch {
	case strings.Contains(m.URL, "censored.tv"):
		// Not implemented
	default:
		// Use default
	}
	args = append(args, writeJsonLocation(m.VideoDirectory)...)
	args = append(args, "--restrict-filenames", "-o", "%(title)s.%(ext)s")
	args = append(args, "--retries", "999", "--retry-sleep", "10")
	args = append(args, "--print", "after_move:%(filepath)s")

	if len(m.CookieSource) > 0 {
		args = append(args, "--cookies-from-browser", m.CookieSource)
	}
	if len(m.ExternalDler) > 0 {
		args = append(args, "--external-downloader", m.ExternalDler)
	}
	if len(m.ExternalDlerArgs) > 0 {
		args = append(args, "--external-downloader-args", m.ExternalDlerArgs)
	}
	if len(m.URL) != 0 {
		args = append(args, m.URL)
	}

	logging.PrintD(1, "Built argument list: %v", args)

	return exec.Command("yt-dlp", args...), nil
}

// writeJsonLocation writes the target directory for the JSON file
func writeJsonLocation(s string) []string {
	if s != "" {
		return []string{"--write-info-json", "-P", s}
	}
	return nil
}
