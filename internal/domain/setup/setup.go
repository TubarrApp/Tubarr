package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"tubarr/internal/domain/consts"
)

const (
	tDir         = ".tubarr"
	tFile        = "tubarr.db"
	logFile      = "tubarr.log"
	benchmarkDir = "benchmark"
)

// File and directory path strings.
var (
	HomeTubarrDir string
	DBFilePath    string
	LogFilePath   string
	BenchmarkDir  string
)

// InitProgFilesDirs initializes necessary program directories and filepaths.
func InitProgFilesDirs() error {
	dir, err := os.UserHomeDir()
	if err != nil {
		return errors.New("failed to get home directory")
	}
	HomeTubarrDir = filepath.Join(dir, tDir)
	if _, err := os.Stat(HomeTubarrDir); os.IsNotExist(err) {
		if err := os.MkdirAll(HomeTubarrDir, consts.PermsHomeTubarrDir); err != nil {
			return fmt.Errorf("failed to make directories: %w", err)
		}
	}

	// Main files
	DBFilePath = filepath.Join(HomeTubarrDir, tFile)
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
