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
	if v == nil || cmdTemplate == nil {
		return false, fmt.Errorf("nil video or command template")
	}

	for attempt := 0; attempt <= retries; attempt++ {
		logging.D(0, "Download attempt %d for URL: %s", attempt+1, v.URL)

		if success, err := tryVDownload(v, cmdTemplate); err != nil {
			logging.E(1, "Attempt %d failed: %v", attempt+1, err)
			if attempt == retries {
				return false, fmt.Errorf("all attempts failed: %v", err)
			}
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		} else if success {
			return true, nil
		}
	}
	return false, fmt.Errorf("download failed after %d attempts", retries)
}

// tryVDownload initiates a video download attempt
func tryVDownload(v *models.Video, cmdTemplate func() (*exec.Cmd, error)) (bool, error) {
	cmd, err := cmdTemplate()
	if err != nil {
		return false, fmt.Errorf("failed to create command: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false, fmt.Errorf("stdout pipe error: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return false, fmt.Errorf("stderr pipe error: %v", err)
	}

	filenameChan := make(chan string, 1)

	go scanVCmdOutput(io.MultiReader(stdout, stderr), filenameChan)

	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("command start error: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		return false, fmt.Errorf("command wait error: %v", err)
	}

	filename := <-filenameChan
	if filename == "" {
		return false, fmt.Errorf("no output filename captured")
	}

	v.VPath = filename

	if err := waitForFile(v.VPath, 5*time.Second); err != nil {
		return false, err
	}

	if err := verifyDownload(v.VPath, v.JPath); err != nil {
		return false, err
	}

	logging.S(0, "Download successful: %s", v.VPath)
	return true, nil
}

// scanVCmdOutput scans the video command output for the video filename
func scanVCmdOutput(r io.Reader, filenameChan chan<- string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Println(line)

		if strings.HasPrefix(line, "/") {
			ext := filepath.Ext(line)
			for _, validExt := range consts.AllVidExtensions {
				if ext == validExt {
					filenameChan <- line
					return
				}
			}
		}
	}
	close(filenameChan)
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
