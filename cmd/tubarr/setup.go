package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"tubarr/internal/database"
	"tubarr/internal/domain/paths"
	"tubarr/internal/repo"
	"tubarr/internal/utils/logging"
)

// initializeApplication sets up the application for the current run.
func initializeApplication() (store *repo.Store, db *sql.DB, progControl *repo.ProgControl, err error) {
	// Setup files/dirs
	if err = paths.InitProgFilesDirs(); err != nil {
		fmt.Printf("Tubarr exiting with error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nMain Tubarr file/dir locations:\n\nDatabase: %s\nLog file: %s\n\n",
		paths.DBFilePath, paths.LogFilePath)

	// Database & stores
	database, err := database.InitDB()
	if err != nil {
		fmt.Printf("Tubarr exiting with error: %v\n", err)
		os.Exit(1)
	}
	store, err = repo.InitStores(database.DB)
	if err != nil {
		fmt.Printf("Tubarr exiting with error: %v\n", err)
		os.Exit(1)
	}

	// Start controller
	progControl = repo.NewProgController(database.DB)
	if progControl.ProcessID, err = progControl.StartTubarr(); err != nil {
		if strings.HasPrefix(err.Error(), "failure:") {
			logging.E("DB %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Tubarr exiting with error: %v\n", err)
		os.Exit(1)
	}

	// Setup logging
	if err := logging.SetupLogging(paths.HomeTubarrDir); err != nil {
		fmt.Printf("could not set up logging, proceeding without: %v", err)
	}

	return store, database.DB, progControl, err
}
