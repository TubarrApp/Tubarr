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
	benchmarkDir = "benchmark" // NEW
)

// File and directory path strings.
var (
	CfgDir       string
	DBFilePath   string
	LogFilePath  string
	BenchmarkDir string // NEW - exported for benchmark package
)

// InitCfgFilesDirs initializes necessary program directories and filepaths.
func InitCfgFilesDirs() error {
	dir, err := os.UserHomeDir()
	if err != nil {
		return errors.New("failed to get home directory")
	}
	CfgDir = filepath.Join(dir, tDir)
	if _, err := os.Stat(CfgDir); os.IsNotExist(err) {
		if err := os.MkdirAll(CfgDir, consts.PermsConfigDir); err != nil {
			return fmt.Errorf("failed to make directories: %w", err)
		}
	}

	// Main files
	DBFilePath = filepath.Join(CfgDir, tFile)
	LogFilePath = filepath.Join(CfgDir, logFile)

	// Benchmark directory
	BenchmarkDir = filepath.Join(CfgDir, benchmarkDir)
	if _, err := os.Stat(BenchmarkDir); os.IsNotExist(err) {
		if err := os.MkdirAll(BenchmarkDir, consts.PermsGenericDir); err != nil {
			return fmt.Errorf("failed to make benchmark directory: %w", err)
		}
	}
	return nil
}
