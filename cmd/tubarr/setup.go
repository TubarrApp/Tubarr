package main

import (
	"database/sql"
	"fmt"
	"strings"
	"tubarr/internal/database"
	"tubarr/internal/repo"
)

// initializeApplication sets up the application for the current run, taking the single-instance lock if lock is set.
func initializeApplication(lock bool) (store *repo.Store, db *sql.DB, progControl *repo.ProgControl, err error) {
	// Database & stores.
	if db, err = database.InitDB(); err != nil {
		return nil, nil, nil, err
	}
	if store, err = repo.InitStores(db); err != nil {
		return nil, nil, nil, err
	}

	// Start controller.
	progControl = repo.NewProgController(db)
	if !lock {
		return store, db, progControl, nil
	}

	// Start Tubarr process.
	if progControl.ProcessID, err = progControl.StartTubarr(); err != nil {
		if strings.HasPrefix(err.Error(), "failure:") {
			err = fmt.Errorf("DB %w", err) // "DB failure: reason"
		}
		return nil, nil, nil, err
	}

	return store, db, progControl, err
}
