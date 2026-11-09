package consts

import "time"

// Heartbeat and health checks
const (
	HeartbeatInterval     = 30 * time.Second
	StaleProcessThreshold = 2 * time.Minute
)

// Retry configuration
const (
	DefaultDownloadRetries = 2 // Times a failed download runs again when its settings don't set Download Retries.
)

// UI and display
const (
	CountdownTickInterval = 1 * time.Second
	MaxDisplayedVideos    = 24
)
