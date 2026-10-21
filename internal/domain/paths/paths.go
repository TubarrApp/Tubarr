// Package paths initializes Tubarr's filepaths, directories, etc.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// File and directory name constants.
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
		if err := os.MkdirAll(HomeTubarrDir, sharedconsts.PermsHomeProgDir); err != nil {
			return fmt.Errorf("failed to make directories: %w", err)
		}
	}

	// Home Metarr dir ~/.metarr
	HomeMetarrDir = filepath.Join(UserHomeDir, mDir)
	if _, err := os.Stat(HomeMetarrDir); os.IsNotExist(err) {
		if err := os.MkdirAll(HomeMetarrDir, sharedconsts.PermsHomeProgDir); err != nil {
			return fmt.Errorf("failed to make directories: %w", err)
		}
	}

	// Main files
	DBFilePath = filepath.Join(HomeTubarrDir, tDBFile)
	TubarrLogFilePath = filepath.Join(HomeTubarrDir, tubarrLogFile)
	MetarrLogFilePath = filepath.Join(HomeMetarrDir, metarrLogFile)

	// Benchmark directory, made once flags are parsed if benchmarking.
	BenchmarkDir = filepath.Join(HomeTubarrDir, benchmarkDir)

	return nil
}
