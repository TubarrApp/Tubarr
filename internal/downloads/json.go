package downloads

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"tubarr/internal/domain/cmdjson"
	"tubarr/internal/utils/logging"
)

// buildJSONCommand builds and returns the argument for downloading metadata files for the given URL.
func (d *Download) buildJSONCommand() *exec.Cmd {

	args := make([]string, 0, 32)

	args = append(args,
		cmdjson.SkipVideo,
		cmdjson.WriteInfoJSON,
		cmdjson.P, d.Video.JSONDir)

	if d.Video.Settings.CookieSource != "" {
		args = append(args, cmdjson.CookieSource, d.Video.Settings.CookieSource)
	}

	if d.Video.Settings.MaxFilesize != "" {
		args = append(args, cmdjson.MaxFilesize, d.Video.Settings.MaxFilesize)
	}

	if d.Video.Settings.ExternalDownloader != "" {
		args = append(args, cmdjson.ExternalDLer, d.Video.Settings.ExternalDownloader)

		if d.Video.Settings.ExternalDownloaderArgs != "" {
			args = append(args, cmdjson.ExternalDLArgs, d.Video.Settings.ExternalDownloaderArgs)
		}
	}

	if d.Video.Settings.Retries != 0 {
		args = append(args, cmdjson.Retries, strconv.Itoa(d.Video.Settings.Retries))
	}

	args = append(args, cmdjson.RestrictFilenames, cmdjson.Output, cmdjson.FilenameSyntax,
		d.Video.URL)

	cmd := exec.CommandContext(d.Context, cmdjson.YTDLP, args...)
	logging.D(1, "Built metadata download command for URL %q:\n%v", d.Video.URL, cmd.String())

	return cmd
}

// executeJSONDownload executes a JSON download command.
func (d *Download) executeJSONDownload(cmd *exec.Cmd) error {
	if cmd == nil {
		return fmt.Errorf("no command built for URL %s", d.Video.URL)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("yt-dlp error for %s: %w\nStderr: %s", d.Video.URL, err, stderr.String())
	}

	// Find the line containing the JSON path
	outputLines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var jsonPath string

	fileLine := outputLines[len(outputLines)-1]
	logging.D(1, "File line captured as: %v", fileLine)

	_, jsonPath, found := strings.Cut(fileLine, ": ")
	if !found || jsonPath == "" {
		logging.D(1, "Full stdout: %s", stdout.String())
		logging.D(1, "Full stderr: %s", stderr.String())
		return fmt.Errorf("could not find JSON file path in output for %s", d.Video.URL)
	}

	d.Video.JSONPath = jsonPath

	// Verify the file exists
	if err := verifyJSONDownload(d.Video.JSONPath); err != nil {
		return err
	}

	logging.I("Successfully saved JSON file to %q", d.Video.JSONPath)
	return nil
}