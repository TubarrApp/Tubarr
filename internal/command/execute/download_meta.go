package command

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

// ExecuteMetaDownload initiates a yt-dlp meta download command
func ExecuteMetaDownload(dl *models.DLs) error {
	if dl.JSONCommand == nil {
		return fmt.Errorf("no command built for URL %s", dl.URL)
	}

	var stdout, stderr bytes.Buffer
	dl.JSONCommand.Stdout = &stdout
	dl.JSONCommand.Stderr = &stderr

	err := dl.JSONCommand.Run()
	if err != nil {
		return fmt.Errorf("yt-dlp error for %s: %v\nStderr: %s", dl.URL, err, stderr.String())
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
		return fmt.Errorf("could not find JSON file path in output for %s", dl.URL)
	}

	dl.JSONPath = jsonPath

	// Verify the file exists
	if _, err := os.Stat(dl.JSONPath); err != nil {
		return fmt.Errorf("JSON file not found at %s: %v", dl.JSONPath, err)
	}

	logging.I("Successfully saved JSON file to '%s'", dl.JSONPath)
	return nil
}
