package downloads

import (
	"context"
	"os"
	"os/exec"
	"sync"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

var ongoingDownloads sync.Map
var avoidURLs sync.Map // Avoid attempting downloads for these URLs (e.g. when bot activity detection triggers)

// activeDownloadContexts tracks cancellation functions for active downloads by video ID
var activeDownloadContexts sync.Map // map[int64]context.CancelFunc

// DownloadType represents the type of download operation.
type DownloadType string

// Denotes the type of file being downloaded.
const (
	TypeJSON  DownloadType = "JSON"
	TypeVideo DownloadType = "video"
)

// Options holds configuration for download operations.
type Options struct {
	MaxRetries       int
	RetryMaxInterval int
}

// DefaultOptions provides sensible defaults.
var DefaultOptions = Options{
	MaxRetries:       3,
	RetryMaxInterval: 20,
}

// VideoDownload encapsulates a video download operation.
type VideoDownload struct {
	Video      *models.Video
	ChannelURL *models.ChannelURL
	Channel    *models.Channel
	DLStore    contracts.DownloadStore
	DLTracker  *DownloadTracker
	Options    Options
	Context    context.Context

	// Private
	cmd      *exec.Cmd
	tempFile string
	mu       sync.Mutex
}

// cleanup safely terminates any running command and cleans up temp files.
func (d *VideoDownload) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Kill running command if exists
	if d.cmd != nil && d.cmd.Process != nil {
		// Try graceful termination first
		if err := d.cmd.Process.Signal(os.Interrupt); err != nil {
			// Force kill if graceful fails
			_ = d.cmd.Process.Kill()
		}
		d.cmd = nil
	}

	// Clean up temp file if exists
	if d.tempFile != "" {
		if err := os.Remove(d.tempFile); err != nil && !os.IsNotExist(err) {
			logger.Pl.W("Failed to remove temp file %s: %v", d.tempFile, err)
		}
		d.tempFile = ""
	}
}

// JSONDownload encapsulates a JSON download operation.
type JSONDownload struct {
	Video      *models.Video
	Channel    *models.Channel
	ChannelURL *models.ChannelURL
	DLTracker  *DownloadTracker
	Options    Options
	Context    context.Context

	// Private
	cmd      *exec.Cmd
	tempFile string
	mu       sync.Mutex
}

// cleanup safely terminates any running command and cleans up temp files.
func (d *JSONDownload) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Kill running command if exists
	if d.cmd != nil && d.cmd.Process != nil {
		// Try graceful termination first
		if err := d.cmd.Process.Signal(os.Interrupt); err != nil {
			// Force kill if graceful fails
			_ = d.cmd.Process.Kill()
		}
		d.cmd = nil
	}

	// Clean up temp file if exists
	if d.tempFile != "" {
		if err := os.Remove(d.tempFile); err != nil && !os.IsNotExist(err) {
			logger.Pl.W("Failed to remove temp file %s: %v", d.tempFile, err)
		}
		d.tempFile = ""
	}
}

// RegisterDownloadContext registers a cancellation function for a video download.
func RegisterDownloadContext(videoID int64, videoURL string, cancel context.CancelFunc) {
	activeDownloadContexts.Store(videoID, cancel)
	logger.Pl.D(2, "Registered cancellation context for video ID %d with URL %q", videoID, videoURL)
}

// UnregisterDownloadContext removes a video's cancellation function.
func UnregisterDownloadContext(videoID int64, videoURL string) {
	activeDownloadContexts.Delete(videoID)
	logger.Pl.D(2, "Unregistered cancellation context for video ID %d with URL %q", videoID, videoURL)
}

// CancelDownloadByVideoID cancels an active download by video ID.
func CancelDownloadByVideoID(videoID int64, videoURL string) bool {
	if cancel, ok := activeDownloadContexts.Load(videoID); ok {
		if cancelFunc, ok := cancel.(context.CancelFunc); ok {
			logger.Pl.I("Cancelling download for video ID %d with URL %q", videoID, videoURL)
			cancelFunc()
			return true
		}
	}
	return false
}
