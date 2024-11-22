package downloads

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// executeJSONDownload initiates a yt-dlp meta download command
func executeJSONDownload(v *models.Video, cmd *exec.Cmd) error {
	if cmd == nil {
		return fmt.Errorf("no command built for URL %s", v.URL)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("yt-dlp error for %s: %v\nStderr: %s", v.URL, err, stderr.String())
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
		return fmt.Errorf("could not find JSON file path in output for %s", v.URL)
	}

	v.JPath = jsonPath

	// Verify the file exists
	if _, err := os.Stat(v.JPath); err != nil {
		return fmt.Errorf("JSON file not found at %s: %v", v.JPath, err)
	}

	logging.I("Successfully saved JSON file to %q", v.JPath)
	return nil
}
