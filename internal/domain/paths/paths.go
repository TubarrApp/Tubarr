// Package paths initializes Tubarr's filepaths, directories, etc.
package paths

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/TubarrApp/gocommon/sharedconsts"
)

// File and directory name constants
const (
	tubarrDir     = ".tubarr"
	tubarrDBFile  = "tubarr.db"
	tubarrLogFile = "tubarr.log"

	metarrDir     = ".metarr"
	metarrLogFile = "metarr.log"

	benchmarkDir = "benchmark"
	uploadDir    = "uploads"
	cacheDir     = "cache"
	thumbnailDir = "thumbnails"
)

// File and directory path strings
var (
	HomeMetarrDir string
	HomeTubarrDir string
	DBFilePath    string
	BenchmarkDir  string

	// Log file paths
	TubarrLogFilePath string
	MetarrLogFilePath string

	// Uploads
	UploadedThumbnailDir string

	// Tubarr cache elements
	CacheThumbnailDir string
)

// InitProgFilesDirs initializes necessary program directories and filepaths.
func InitProgFilesDirs() error {
	UserHomeDir, err := os.UserHomeDir()
	if err != nil {
		return errors.New("failed to get home directory")
	}

	// Home Tubarr dir ~/.tubarr
	HomeTubarrDir = filepath.Join(UserHomeDir, tubarrDir)
	if _, err := os.Stat(HomeTubarrDir); os.IsNotExist(err) {
		if err := os.MkdirAll(HomeTubarrDir, sharedconsts.PermsHomeProgDir); err != nil {
			return fmt.Errorf("failed to make directories: %w", err)
		}
	}

	// Home Metarr dir ~/.metarr
	HomeMetarrDir = filepath.Join(UserHomeDir, metarrDir)
	if _, err := os.Stat(HomeMetarrDir); os.IsNotExist(err) {
		if err := os.MkdirAll(HomeMetarrDir, sharedconsts.PermsHomeProgDir); err != nil {
			return fmt.Errorf("failed to make directories: %w", err)
		}
	}

	// Main files
	DBFilePath = filepath.Join(HomeTubarrDir, tubarrDBFile)
	TubarrLogFilePath = filepath.Join(HomeTubarrDir, tubarrLogFile)
	MetarrLogFilePath = filepath.Join(HomeMetarrDir, metarrLogFile)

	// Benchmark directory, made once flags are parsed if benchmarking
	BenchmarkDir = filepath.Join(HomeTubarrDir, benchmarkDir)

	// Uploads directory for holding files uploaded by the user
	UploadedThumbnailDir = filepath.Join(HomeTubarrDir, uploadDir, thumbnailDir)

	// Cache directory holding items cached by Tubarr
	CacheThumbnailDir = filepath.Join(HomeTubarrDir, cacheDir, thumbnailDir)

	return nil
}
