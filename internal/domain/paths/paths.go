// Package paths initializes Tubarr's filepaths, directories, etc.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"tubarr/internal/abstractions"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
)

const (
	mDir          = ".metarr"
	tDir          = ".tubarr"
	tDBFile       = "tubarr.db"
	tubarrLogFile = "tubarr.log"
	metarrLogFile = "metarr.log"
	benchmarkDir  = "benchmark"
)

// File and directory path strings.
var (
	HomeMetarrDir     string
	HomeTubarrDir     string
	DBFilePath        string
	TubarrLogFilePath string
	MetarrLogFilePath string
	BenchmarkDir      string
)

// InitProgFilesDirs initializes necessary program directories and filepaths.
func InitProgFilesDirs() error {
	UserHomeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.New("failed to get home directory")
	}

	// Home Tubarr dir ~/.tubarr
	HomeTubarrDir = filepath.Join(UserHomeDir, tDir)
	if _, err := os.Stat(HomeTubarrDir); os.IsNotExist(err) {
		if err := os.MkdirAll(HomeTubarrDir, consts.PermsHomeProgDir); err != nil {
			return fmt.Errorf("failed to make directories: %w", err)
		}
	}

	// Home Metarr dir ~/.metarr
	HomeMetarrDir = filepath.Join(UserHomeDir, mDir)
	if _, err := os.Stat(HomeMetarrDir); os.IsNotExist(err) {
		if err := os.MkdirAll(HomeMetarrDir, consts.PermsHomeProgDir); err != nil {
			return fmt.Errorf("failed to make directories: %w", err)
		}
	}

	// Main files
	DBFilePath = filepath.Join(HomeTubarrDir, tDBFile)
	TubarrLogFilePath = filepath.Join(HomeTubarrDir, tubarrLogFile)
	MetarrLogFilePath = filepath.Join(HomeMetarrDir, metarrLogFile)

	// Benchmark directory
	if abstractions.GetBool(keys.Benchmarking) {
		BenchmarkDir = filepath.Join(HomeTubarrDir, benchmarkDir)
		if _, err := os.Stat(BenchmarkDir); os.IsNotExist(err) {
			if err := os.MkdirAll(BenchmarkDir, consts.PermsGenericDir); err != nil {
				return fmt.Errorf("failed to make benchmark directory: %w", err)
			}
		}
	}
	return nil
}
