package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	tDir = ".tubarr"

	tFile   = "tubarr.db"
	logFile = "tubarr.log"
)

var (
	CfgDir      string
	DBFilePath  string
	LogFilePath string
)

// InitCfgFilesDirs initializes necessary program directories and filepaths.
func InitCfgFilesDirs() error {

	dir, err := os.UserHomeDir()
	if err != nil {
		return errors.New("failed to get home directory")
	}
	CfgDir = filepath.Join(dir, tDir)

	if _, err := os.Stat(CfgDir); os.IsNotExist(err) {
		if err := os.MkdirAll(CfgDir, 0o755); err != nil {
			return fmt.Errorf("failed to make directories: %v", err)
		}
	}

	DBFilePath = filepath.Join(CfgDir, tFile)
	LogFilePath = filepath.Join(CfgDir, logFile)

	return nil
}
