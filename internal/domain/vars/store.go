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
	// MaxMetarrLogs matches the gocommon/logging buffer size to maintain consistency.
	MaxMetarrLogs = 2500
)

// Metarr
var (
	MetarrLogs      [][]byte
	MetarrLogsMutex sync.RWMutex
	MetarrFinished  bool
)

// BlockContext represents the authentication context for domain blocking.
// Domains are blocked per-context since auth/cookies may have different rate limits.
type BlockContext string

const (
	// BlockContextUnauth represents unauthenticated access (no cookies, no auth).
	BlockContextUnauth BlockContext = "unauth"
	// BlockContextCookie represents cookie-based authentication.
	BlockContextCookie BlockContext = "cookie"
	// BlockContextAuth represents username/password authentication.
	BlockContextAuth BlockContext = "auth"
)

// Global bot blocking - tracks which domains have blocked Tubarr and when.
// Blocks are context-aware: a block on unauthenticated access doesn't affect
// authenticated channels, and vice versa.
var (
	// BlockedDomains: domain -> (context -> timestamp when blocked)
	BlockedDomains      map[string]map[BlockContext]time.Time
	BlockedDomainsMutex sync.RWMutex
)
