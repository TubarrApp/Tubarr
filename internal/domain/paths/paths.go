// Package paths initializes Tubarr's filepaths, directories, etc.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"tubarr/internal/domain/consts"
)

const (
	tDir         = ".tubarr"
	tDBFile      = "tubarr.db"
	logFile      = "tubarr.log"
	benchmarkDir = "benchmark"
)

// File and directory path strings.
var (
	UserHomeDir   string
	HomeTubarrDir string
	DBFilePath    string
	LogFilePath   string
	BenchmarkDir  string
)

// InitProgFilesDirs initializes necessary program directories and filepaths.
func InitProgFilesDirs() error {
	UserHomeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.New("failed to get home directory")
	}
	HomeTubarrDir = filepath.Join(UserHomeDir, tDir)
	if _, err := os.Stat(HomeTubarrDir); os.IsNotExist(err) {
		if err := os.MkdirAll(HomeTubarrDir, consts.PermsHomeTubarrDir); err != nil {
			return fmt.Errorf("failed to make directories: %w", err)
		} else {
			return fmt.Errorf("failed to stat home directory %q", HomeTubarrDir)
		}
	}

	// Main files
	DBFilePath = filepath.Join(HomeTubarrDir, tDBFile)
	LogFilePath = filepath.Join(HomeTubarrDir, logFile)

	// Benchmark directory
	BenchmarkDir = filepath.Join(HomeTubarrDir, benchmarkDir)
	if _, err := os.Stat(BenchmarkDir); os.IsNotExist(err) {
		if err := os.MkdirAll(BenchmarkDir, consts.PermsGenericDir); err != nil {
			return fmt.Errorf("failed to make benchmark directory: %w", err)
		}
	}
	return nil
}
