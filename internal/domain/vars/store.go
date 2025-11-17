// Package vars stores global variables.
package vars

import "github.com/TubarrApp/gocommon/benchmark"

// BenchmarkFiles holds the global pointer to BenchFiles.
var BenchmarkFiles *benchmark.BenchFiles

// Metarr
var (
	MetarrLogs     [][]byte
	MetarrFinished bool
)
