package consts

import "time"

// Heartbeat and health checks
const (
	HeartbeatInterval     = 30 * time.Second
	StaleProcessThreshold = 2 * time.Minute
)

// Network timeouts
const (
	HTTPClientTimeout = 10 * time.Second
	ScraperTimeout    = 60 * time.Second
	DatabaseTimeout   = 5 * time.Second
)

// Retry configuration
const (
	DefaultMaxRetries = 3
	RetryInterval     = 5 * time.Second
	RetryBackoff      = 100 * time.Millisecond
)

// File operations
const (
	FileCheckInterval = 100 * time.Millisecond
	FileWaitTimeout   = 10 * time.Second
)

// UI and display
const (
	CountdownTickInterval = 1 * time.Second
	MaxDisplayedVideos    = 24
)
