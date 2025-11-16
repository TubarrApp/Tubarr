package main

import (
	"database/sql"
	"os"
	"strings"
	"tubarr/internal/database"
	"tubarr/internal/domain/logger"
	"tubarr/internal/repo"
)

// initializeApplication sets up the application for the current run.
func initializeApplication() (store *repo.Store, db *sql.DB, progControl *repo.ProgControl, err error) {
	// Database & stores
	database, err := database.InitDB()
	if err != nil {
		logger.Pl.P("Tubarr exiting with error: %v\n", err)
		os.Exit(1)
	}
	store, err = repo.InitStores(database.DB)
	if err != nil {
		logger.Pl.P("Tubarr exiting with error: %v\n", err)
		os.Exit(1)
	}

	// Start controller
	progControl = repo.NewProgController(database.DB)
	if progControl.ProcessID, err = progControl.StartTubarr(); err != nil {
		if strings.HasPrefix(err.Error(), "failure:") {
			logger.Pl.E("DB %v\n", err)
			os.Exit(1)
		}
		logger.Pl.P("Tubarr exiting with error: %v\n", err)
		os.Exit(1)
	}

	return store, database.DB, progControl, err
}
