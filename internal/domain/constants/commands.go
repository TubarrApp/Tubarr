package domain

import (
	"Tubarr/internal/models"
	"os/exec"
	"strings"
)

var ()

func GrabLatestCommand(vDir, url, cookies, eDl, eDlArgs string, d models.DownloadedFiles) *exec.Cmd {

	var args []string

	switch {
	case strings.Contains(d.URL, "censored.tv"):
		// Not implemented
	default:
		// Use default
	}

	args = append(args, writeJsonLocation(vDir)...)
	args = append(args, "--restrict-filenames", "-o", "%(title)s.%(ext)s")
	args = append(args, "--retries", "999", "--retry-sleep", "10")
	args = append(args, "--print", "filename")

	if len(cookies) > 0 {
		args = append(args, "--cookies-from-browser", cookies)
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
	return exec.Command("yt-dlp", args...)
}

func writeJsonLocation(s string) []string {
	if s != "" {
		return []string{"--write-info-json", "-P", s}
	}
	return nil
}
