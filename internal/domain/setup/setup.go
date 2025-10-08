// Package setup handles initialization of the software, such as creating filepaths and directories.
package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"tubarr/internal/domain/consts"
)

const (
	tDir = ".tubarr"

	tFile   = "tubarr.db"
	logFile = "tubarr.log"
)

// File and directory path strings.
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
		if err := os.MkdirAll(CfgDir, consts.PermsConfigFile); err != nil {
			return fmt.Errorf("failed to make directories: %w", err)
		}
	}

	// Main files
	DBFilePath = filepath.Join(CfgDir, tFile)
	LogFilePath = filepath.Join(CfgDir, logFile)

	return nil
}
