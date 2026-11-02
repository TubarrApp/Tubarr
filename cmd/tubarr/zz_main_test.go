package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/TubarrApp/gocommon/logging"
)

var testPl *logging.ProgramLogger

// TestMain initializes the logger, logging to a temporary folder.
func TestMain(m *testing.M) {
	logDir, err := os.MkdirTemp("", "tubarr-test-*")
	if err != nil {
		fmt.Printf("Tubarr exiting with error: %v\n", err)
		os.Exit(1)
	}

	// Setup Tubarr logging.
	logConfig := logging.Config{
		LogFilePath: filepath.Join(logDir, "tubarr.log"),
		MaxSizeMB:   1,
		MaxBackups:  3,
		Console:     os.Stdout,
		Program:     "Tubarr",
	}

	if testPl, err = logging.SetupLogging(logConfig); err != nil {
		fmt.Printf("Tubarr exiting with error: %v\n", err)
		os.Exit(1)
	}

	code := m.Run()
	if err := os.RemoveAll(logDir); err != nil {
		fmt.Printf("Failed to remove %q: %v\n", logDir, err)
	}
	os.Exit(code)
}

// Test logger enters gocommon without panic.
func TestGocommonLogger(t *testing.T) {
	logging.TestLogger(testPl)
}
