package downloads

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
	"tubarr/internal/domain/consts"
)

// verifyJSONDownload verifies the JSON file downloaded and contains valid JSON data.
func verifyJSONDownload(jsonPath string) error {
	jsonData, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("failed to read JSON file: %w", err)
	}

	if !json.Valid(jsonData) {
		return fmt.Errorf("invalid JSON content in file: %s", jsonPath)
	}
	return nil
}

// verifyVideoDownload checks if the specified video file exists and is not empty.
func verifyVideoDownload(videoPath string) error {
	// Check video file
	videoInfo, err := os.Stat(videoPath)
	if err != nil {
		return fmt.Errorf("video file verification failed: %w", err)
	}
	if videoInfo.Size() == 0 {
		return fmt.Errorf("video file is empty: %s", videoPath)
	}

	return nil
}

// waitForFile waits until the file is ready in the file system.
func (d *Download) waitForFile(filepath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {

		if _, err := os.Stat(filepath); err == nil { // err IS nil
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("unexpected error while checking file: %w", err)
		}

		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("file not ready or empty after %v: %s", timeout, filepath)
}

// cancelDownload cancels the download, typically due to user input.
func (d *Download) cancelDownload() error {
	d.Video.DownloadStatus.Status = consts.DLStatusFailed
	d.Video.DownloadStatus.Error = d.Context.Err()
	d.DLTracker.sendUpdate(d.Video)
	return fmt.Errorf("user canceled download for %s: %w", d.Video.URL, d.Context.Err())
}
