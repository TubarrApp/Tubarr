package command

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

// ExecuteVideoDownload takes in a URL and downloads it
func ExecuteVideoDownload(v *models.Video, cmd *exec.Cmd) (bool, error) {
	if v == nil {
		return false, fmt.Errorf("request model received nil")
	}

	if cmd == nil {
		return false, fmt.Errorf("command is nil for model with URL '%s'", v.URL)
	}

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logging.E(0, "Failed to create stdout pipe: %v", err)
		return false, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		logging.E(0, "Failed to create stderr pipe: %v", err)
		return false, err
	}

	// Channel to receive the output filename
	filenameChan := make(chan string, 1)
	doneChan := make(chan struct{})
	errChan := make(chan error, 1)

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

				for _, ext := range consts.AllVidExtensions {
					logging.D(2, "Comparing extension %s to %s", ext, lineExt)
					if lineExt == ext {
						select {
						case filenameChan <- line:
						default:
							logging.D(1, "Channel already has a filename")
						}
						break
					}
				}
			}
		}
		if err := scanner.Err(); err != nil {
			errChan <- fmt.Errorf("scanner error: %v", err)
		}
	}()

	// Start command
	logging.I("Executing download command: %s", cmd.String())
	if err := cmd.Start(); err != nil {
		close(filenameChan)
		return false, fmt.Errorf("failed to start download: %v", err)
	}

	// Wait for command to complete
	if err := cmd.Wait(); err != nil {
		close(filenameChan)
		return false, fmt.Errorf("download failed: %v", err)
	}

	// Wait for scanner to finish
	<-doneChan

	// Check for scanner errors
	select {
	case err := <-errChan:
		if err != nil {
			return false, err
		}
	default:
	}

	// Get output filename
	var outputFilename string
	select {
	case filename := <-filenameChan:
		outputFilename = filename
		close(filenameChan)
	default:
		close(filenameChan)
		return false, fmt.Errorf("no output filename captured for URL: %s", v.URL)
	}

	// Short wait time for filesystem sync
	time.Sleep(1 * time.Second)

	// Set filenames
	v.VPath = outputFilename

	// Verify the download
	if err := verifyDownload(v.VPath, v.JPath); err != nil {
		return false, fmt.Errorf("download verification failed: %v", err)
	}

	logging.S(0, "Successfully downloaded files:\nVideo: %s\nJSON: %s",
		v.VPath, v.JPath)

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
