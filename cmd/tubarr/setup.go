package main

import (
	"fmt"
	"os"
	"strings"
	"tubarr/internal/database"
	"tubarr/internal/domain/setup"
	"tubarr/internal/repo"
	"tubarr/internal/utils/logging"
)

// initializeApplication sets up the application for the current run.
func initializeApplication() (store *repo.Store, progControl *repo.ProgControl, err error) {
	// Setup files/dirs
	if err = setup.InitCfgFilesDirs(); err != nil {
		fmt.Printf("Tubarr exiting: %v\n", err)
		os.Exit(0)
	}

	fmt.Printf("\nMain Tubarr file/dir locations:\n\nDatabase: %s\nLog file: %s\n\n",
		setup.DBFilePath, setup.LogFilePath)

	// Database & stores
	database, err := database.InitDB()
	if err != nil {
		fmt.Printf("Tubarr exiting: %v\n", err)
		os.Exit(0)
	}
	store = repo.InitStores(database.DB)

	// Start controller
	progControl = repo.NewProgController(database.DB)
	if progControl.ProcessID, err = progControl.StartTubarr(); err != nil {
		if strings.HasPrefix(err.Error(), "failure:") {
			logging.E("DB %v\n", err)
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
