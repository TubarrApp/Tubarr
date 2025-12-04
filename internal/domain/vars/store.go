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
	MetarrFinished  bool
)

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
	BlockedDomains      map[string]map[BlockContext]time.Time
	BlockedDomainsMutex sync.RWMutex
)
