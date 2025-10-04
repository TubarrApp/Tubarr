package downloads

import (
	"context"
	"sync"
	"time"

	"tubarr/internal/interfaces"
	"tubarr/internal/models"
)

var ongoingDownloads sync.Map

// DownloadType represents the type of download operation.
type DownloadType string

const (
	TypeJSON  DownloadType = "JSON"
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

// VideoDownload encapsulates a video download operation.
type VideoDownload struct {
	Video      *models.Video
	ChannelURL *models.ChannelURL
	Channel    *models.Channel
	DLStore    interfaces.DownloadStore
	DLTracker  *DownloadTracker
	Options    Options
	Context    context.Context
}

// JSONDownload encapsulates a JSON download operation.
type JSONDownload struct {
	Video      *models.Video
	Channel    *models.Channel
	ChannelURL *models.ChannelURL
	DLTracker  *DownloadTracker
	Options    Options
	Context    context.Context
}
