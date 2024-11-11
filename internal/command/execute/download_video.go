package command

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	consts "tubarr/internal/domain/constants"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

// ExecuteVideoDownload takes in a URL and downloads it
func ExecuteVideoDownload(request *models.DLs) (bool, error) {
	if request == nil {
		return false, fmt.Errorf("request model received nil")
	}

	cmd := request.VideoCommand
	if cmd == nil {
		return false, fmt.Errorf("command is nil for model with URL '%s'", request.URL)
	}

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logging.E(0, "Failed to create stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		logging.E(0, "Failed to create stderr pipe: %v", err)
	}

	// Channel to receive the output filename
	filenameChan := make(chan string, 1)
	doneChan := make(chan struct{})

	// Start output scanner in goroutine
	go func() {
		defer close(doneChan)
		scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
		for scanner.Scan() {
			line := scanner.Text()
			fmt.Println(line)

			// Capture the actual output filename
			if strings.HasPrefix(line, "/") {
				lineExt := filepath.Ext(line)

				var match bool
				for _, ext := range consts.AllVidExtensions {
					logging.D(2, "Comparing extension %s to %s", ext, lineExt)
					if lineExt == ext {
						match = true
						select {
						case filenameChan <- line:
						default:
							// Channel already has a filename
						}
					}
					if !match {
						logging.D(1, "Did not find yt-dlp supported video format in output %s", line)
					}
				}
			}
			if err := scanner.Err(); err != nil {
				logging.E(0, "Scanner error: %v", err)
			}
		}
	}()

	// Start command
	logging.I("Executing download command: %s", cmd.String())
	if err := cmd.Start(); err != nil {
		logging.E(0, "Failed to start download: %v", err)
		close(filenameChan)
	}

	// Wait for command to complete
	if err := cmd.Wait(); err != nil {
		logging.E(0, "Download failed: %v", err)
		close(filenameChan)
	}

	// Wait for scanner to finish
	<-doneChan
	close(filenameChan)

	// Get output filename
	outputFilename := ""
	select {
	case filename := <-filenameChan:
		outputFilename = filename
	default:
		logging.E(0, "No output filename captured for URL: %s", request.URL)
	}

	// Short wait time for filesystem sync
	time.Sleep(1 * time.Second)

	// Set filenames
	request.VideoPath = outputFilename

	// Verify the download
	if err := verifyDownload(request.VideoPath, request.JSONPath); err != nil {
		logging.E(0, "Download verification failed: %v", err)
	}

	logging.S(0, "Successfully downloaded files:\nVideo: %s\nJSON: %s",
		request.VideoPath, request.JSONPath)

	return true, nil
}

// verifyDownload checks if the specified files exist and are non-empty
func verifyDownload(videoPath, jsonPath string) error {
	// Check video file
	videoInfo, err := os.Stat(videoPath)
	if err != nil {
		return fmt.Errorf("video file verification failed: %w", err)
	}
	if videoInfo.Size() == 0 {
		return fmt.Errorf("video file is empty: %s", videoPath)
	}

	// Check JSON file
	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to read JSON file: %w", err)
	}

	if !json.Valid(jsonData) {
		return fmt.Errorf("invalid JSON content in file: %s", jsonPath)
	}

	return nil
}
