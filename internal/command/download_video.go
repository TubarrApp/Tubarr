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
func ExecuteVideoDownload(v *models.Video, cmdTemplate func() (*exec.Cmd, error), retries int) (bool, error) {
	if v == nil {
		return false, fmt.Errorf("request model received nil")
	}

	if cmdTemplate == nil {
		return false, fmt.Errorf("command template function is nil for model with URL '%s'", v.URL)
	}

	var lastError error

	for attempt := 0; attempt <= retries; attempt++ {
		logging.D(0, "Attempt %d to download video from URL: %s", attempt+1, v.URL)

		// Create a new command for each attempt
		cmd, err := cmdTemplate()
		if err != nil || cmd == nil {
			return false, fmt.Errorf("failed to create new command instance for attempt %d", attempt+1)
		}

		// Create pipes for stdout and stderr
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return false, fmt.Errorf("failed to create stdout pipe: %v", err)
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return false, fmt.Errorf("failed to create stderr pipe: %v", err)
		}

		// Channel to receive the output filename
		filenameChan := make(chan string, 1)
		errChan := make(chan error, 1)

		// Start the command before starting the scanner goroutine
		if err := cmd.Start(); err != nil {
			lastError = fmt.Errorf("failed to start download: %v", err)
			logging.E(1, "Download attempt %d failed to start: %v", attempt+1, lastError)
			continue
		}

		// Start output scanner in a goroutine
		go func() {
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
				select {
				case errChan <- fmt.Errorf("scanner error: %v", err):
				default:
				}
			}
			close(filenameChan)
		}()

		// Wait for command to complete
		if err := cmd.Wait(); err != nil {
			lastError = fmt.Errorf("download failed on attempt %d: %v", attempt+1, err)
			logging.E(1, "Download attempt %d failed: %v", attempt+1, lastError)
			continue
		}

		// Check for scanner errors
		select {
		case err := <-errChan:
			if err != nil {
				lastError = fmt.Errorf("scanner error on attempt %d: %v", attempt+1, err)
				logging.E(1, "Scanner error on attempt %d: %v", attempt+1, lastError)
				continue
			}
		default:
		}

		// Get output filename
		var outputFilename string
		select {
		case filename, ok := <-filenameChan:
			if !ok {
				lastError = fmt.Errorf("filename channel closed without result for URL: %s on attempt %d", v.URL, attempt+1)
				logging.E(1, "Filename channel closed without result on attempt %d", attempt+1)
				continue
			}
			outputFilename = filename
		default:
			lastError = fmt.Errorf("no output filename captured for URL: %s on attempt %d", v.URL, attempt+1)
			logging.E(1, "No output filename captured on attempt %d", attempt+1)
			continue
		}

		// Wait for file sync
		if err := waitForFile(outputFilename, 5*time.Second); err != nil {
			lastError = fmt.Errorf("filesystem sync failed on attempt %d: %v", attempt+1, err)
			logging.E(1, "Filesystem sync failed on attempt %d: %v", attempt+1, lastError)
			continue
		}

		// Set filenames
		v.VPath = outputFilename

		// Verify the download
		if err := verifyDownload(v.VPath, v.JPath); err != nil {
			lastError = fmt.Errorf("download verification failed on attempt %d: %v", attempt+1, err)
			logging.E(1, "Download verification failed on attempt %d: %v", attempt+1, lastError)
			continue
		}

		// Success
		logging.S(0, "Successfully downloaded files:\nVideo: %s\nJSON: %s", v.VPath, v.JPath)
		return true, nil
	}

	// All attempts failed
	return false, fmt.Errorf("all attempts failed for URL %s: %v", v.URL, lastError)
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
