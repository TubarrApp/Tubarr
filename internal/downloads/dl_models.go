package downloads

import (
	"context"
	"time"

	"tubarr/internal/interfaces"
	"tubarr/internal/models"
)

// DownloadType represents the type of download operation.
type DownloadType string

const (
	TypeJSON  DownloadType = "json"
	TypeVideo DownloadType = "video"
)

// Options holds configuration for download operations.
type Options struct {
	MaxRetries    int
	RetryInterval time.Duration
}

// DefaultOptions provides sensible defaults.
var DefaultOptions = Options{
	MaxRetries:    3,
	RetryInterval: 5 * time.Second,
}

// Download encapsulates a download operation.
type Download struct {
	Type      DownloadType
	Video     *models.Video
	DLStore   interfaces.DownloadStore
	DLTracker *DownloadTracker
	Options   Options
	Context   context.Context
}
