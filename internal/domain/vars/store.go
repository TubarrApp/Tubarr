// Package vars stores global variables.
package vars

import (
	"sync"
	"time"

	"github.com/TubarrApp/gocommon/benchmark"
)

// BenchmarkFiles holds the global pointer to BenchFiles.
var BenchmarkFiles *benchmark.BenchFiles

// Metarr log constants.
const (
	MaxMetarrLogs = 2500
)

// Metarr variables.
var (
	MetarrLogs      [][]byte
	MetarrLogsMutex sync.RWMutex
	MetarrLogToken  string // Passed to Metarr, which sends it with its logs. Empty outside web mode.
	MetarrLogURL    string // Passed to Metarr, where it sends its logs. Empty outside web mode.
)

// BlockedDomain contains information about a domain block.
type BlockedDomain struct {
	BlockedAt     time.Time
	BlockedVideos []BlockedVideo
}

// BlockedVideo contains information about a video that failed from a block.
type BlockedVideo struct {
	ID        int64 `json:"id"`
	ChannelID int64 `json:"channel_id"`
}

// BlockContext represents the authentication context for domain blocking.
type BlockContext string

// Contexts for blocking.
const (
	BlockContextUnauth BlockContext = "unauth" // No cookies no auth.
	BlockContextCookie BlockContext = "cookie" // Cookies, no auth.
	BlockContextAuth   BlockContext = "auth"   // Direct auth credentials.
)

// Global bot blocking.
var (
	BlockedDomains      map[string]map[BlockContext]BlockedDomain
	BlockedDomainsMutex sync.RWMutex
)
