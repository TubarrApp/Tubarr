package downloads

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
	"tubarr/internal/utils/logging"
)

// ExecuteVideoDownload takes in a URL and downloads it
func executeVideoDownload(v *models.Video, cmd *exec.Cmd) error {

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe error: %v", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe error: %v", err)
	}

	filenameChan := make(chan string, 1)

	go scanVCmdOutput(io.MultiReader(stdout, stderr), filenameChan)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("command start error: %v", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("command wait error: %v", err)
	}

	filename := <-filenameChan
	if filename == "" {
		return fmt.Errorf("no output filename captured")
	}

	v.VPath = filename

	if err := waitForFile(v.VPath, 5*time.Second); err != nil {
		return err
	}

	if err := verifyDownload(v.VPath, v.JPath); err != nil {
		return err
	}

	logging.S(0, "Download successful: %s", v.VPath)
	return nil
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
