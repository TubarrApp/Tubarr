package main

import (
	"fmt"
	"os"
	"testing"
	"tubarr/internal/domain/paths"

	"github.com/TubarrApp/gocommon/logging"
)

var testPl *logging.ProgramLogger

// TestMain initializes the logger.
func TestMain(m *testing.M) {
	// Setup Tubarr logging.
	logConfig := logging.Config{
		LogFilePath: paths.TubarrLogFilePath,
		MaxSizeMB:   1,
		MaxBackups:  3,
		Console:     os.Stdout,
		Program:     "Tubarr",
	}

	var err error
	if testPl, err = logging.SetupLogging(logConfig); err != nil {
		fmt.Printf("Tubarr exiting with error: %v\n", err)
		os.Exit(1)
	}
}

// Test logger enters gocommon without panic.
func TestGocommonLogger(t *testing.T) {
	logging.TestLogger(testPl)
}
