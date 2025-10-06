package downloads

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/utils/logging"
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
	if videoInfo.IsDir() {
		return fmt.Errorf("dev error: video path created is a directory")
	}

	return nil
}

// waitForFile waits until the file is ready in the file system.
func (d *VideoDownload) waitForFile(filepath string, timeout time.Duration) error {
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

// cancelVideoDownload cancels the download, typically due to user input.
func (d *VideoDownload) cancelVideoDownload() error {
	d.Video.DownloadStatus.Status = consts.DLStatusFailed
	d.Video.DownloadStatus.Error = d.Context.Err()
	d.DLTracker.sendUpdate(d.Video)
	return fmt.Errorf("user canceled video download for %s: %w", d.Video.URL, d.Context.Err())
}

// cancelJSONDownload cancels the download, typically due to user input.
func (d *JSONDownload) cancelJSONDownload() error {
	return fmt.Errorf("user canceled JSON download for %s: %w", d.Video.URL, d.Context.Err())
}

// checkBotDetection checks and handles detection of bot activity.
func checkBotDetection(uri string, inputErr error) error {
	if strings.Contains(strings.ToLower(inputErr.Error()), "confirm you’re not a bot") || // Curly apostrophe (used by YouTube)
		strings.Contains(strings.ToLower(inputErr.Error()), "confirm you're not a bot") || // Straight apostrophe
		strings.Contains(strings.ToLower(inputErr.Error()), "not a robot") {

		siteHost, err := url.Parse(uri) // Avoid using the 'ChannelURL' URL, which can be the manual download value
		if err != nil {
			logging.E("Failed to parse URL %q after bot detection", uri)
		}

		avoidURLs.Store(siteHost.Hostname(), true)
		return fmt.Errorf("url %q %s. Aborting without retries. Error message: %w", uri, consts.BotActivitySentinel, inputErr)
	}
	return nil
}

// checkIfAvoidURL checks if the hostname of the URL should be skipped.
func checkIfAvoidURL(uri string) error {
	var siteHost string

	parsedURL, err := url.Parse(uri)
	if err != nil {
		logging.E("Could not parse URL %q, will check against full URL instead")
		siteHost = uri
	} else {
		siteHost = parsedURL.Hostname()
	}

	if _, exists := avoidURLs.Load(siteHost); exists {
		return fmt.Errorf("skipping download for %q due to site %q detecting bot activity", uri, siteHost)
	}

	return nil
}
