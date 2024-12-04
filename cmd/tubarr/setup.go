package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"tubarr/internal/data/database"
	"tubarr/internal/data/repo"
	"tubarr/internal/domain/setup"
	"tubarr/internal/utils/benchmark"
	"tubarr/internal/utils/logging"
)

// initializeApplication sets up the application for the current run.
func initializeApplication(startTime time.Time) (store *repo.Store, progControl *repo.ProgControl, err error) {

	// Get directory of main.go (helpful for benchmarking file save locations)
	_, mainGoPath, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintf(os.Stderr, "Error getting current working directory. Got: %v\n", mainGoPath)
		os.Exit(1)
	}
	benchmark.InjectMainWorkDir(mainGoPath)

	// Setup files/dirs
	if err := setup.InitCfgFilesDirs(startTime.Format("2006-01-02 15:04:05.00 MST")); err != nil {
		fmt.Printf("Tubarr exiting: %v\n", err)
		os.Exit(0)
	}

	fmt.Printf("\nMain Tubarr file/dir locations:\n\nDatabase: %s\nLog file: %s\n\n",
		setup.DBFilePath, setup.LogFilePath)

	// Database & stores
	db, err := database.InitDB()
	if err != nil {
		fmt.Printf("Tubarr exiting: %v\n", err)
		os.Exit(0)
	}
	store = repo.InitStores(db.DB)

	// Start controller
	progControl = repo.NewProgController(db.DB)
	if progControl.ProcessID, err = progControl.StartTubarr(); err != nil {
		if strings.HasPrefix(err.Error(), "failure:") {
			logging.E(0, "DB %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Tubarr exiting: %v\n", err)
		os.Exit(0)
	}

	// Setup logging
	if err := logging.SetupLogging(setup.CfgDir); err != nil {
		fmt.Printf("could not set up logging, proceeding without: %v", err)
	}

	return store, progControl, err
}
