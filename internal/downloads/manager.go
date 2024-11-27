// Package downloads handles file downloading commands and operations.
package downloads

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// NewDownload creates a download operation with specified options.
func NewDownload(dlType DownloadType, video *models.Video, opts *Options) (*Download, error) {
	if video == nil {
		return nil, errors.New("video cannot be nil")
	}

	dl := &Download{
		Type:  dlType,
		Video: video,
	}

	if opts != nil {
		dl.Options = *opts
	} else {
		dl.Options = DefaultOptions
	}

	return dl, nil
}

// Execute performs the download with retries.
func (d *Download) Execute() error {
	if d.Video == nil {
		return errors.New("video model is nil")
	}

	var lastErr error
	for attempt := 1; attempt <= d.Options.MaxRetries; attempt++ {
		logging.I("Starting %s download attempt %d/%d for URL: %s",
			d.Type, attempt, d.Options.MaxRetries, d.Video.URL)

		if err := d.executeAttempt(); err != nil {
			lastErr = err
			logging.E(0, "Download attempt %d failed: %v", attempt, err)

			if attempt < d.Options.MaxRetries {
				time.Sleep(d.Options.RetryInterval)
				continue
			}
		} else {
			logging.S(0, "Successfully completed %s download for URL: %s", d.Type, d.Video.URL)
			return nil
		}
	}

	return fmt.Errorf("all %d download attempts failed for %s: %w",
		d.Options.MaxRetries, d.Video.URL, lastErr)
}

// executeAttempt performs a single download attempt.
func (d *Download) executeAttempt() error {
	var cmd *exec.Cmd

	switch d.Type {
	case TypeJSON:
		cmd = buildJSONCommand(d.Video)
	case TypeVideo:
		cmd = buildVideoCommand(d.Video)
	default:
		return fmt.Errorf("unsupported download type: %s", d.Type)
	}

	if d.Type == TypeJSON {
		return executeJSONDownload(d.Video, cmd)
	}
	return executeVideoDownload(d.Video, cmd)
}

// waitForFile waits until the file is ready in the file system.
func waitForFile(filePath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {

		_, err := os.Stat(filePath)
		if err == nil { // If error IS nil
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("unexpected error while checking file: %w", err)
		}

		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("file not ready or empty after %v: %s", timeout, filePath)
}
