package main

import (
	"database/sql"
	"fmt"
	"strings"
	"tubarr/internal/database"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
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

	// Holding the lock, so any video still mid-download or mid-Metarr was left by a previous run.
	if pending, failed, recoverErr := store.VideoStore().RecoverInterrupted(); recoverErr != nil {
		logger.Pl.E("Failed to recover videos the last run stopped partway: %v", recoverErr)
	} else if pending+failed > 0 {
		logger.Pl.I("Recovered videos the last run stopped partway: %d finished with Metarr pending, %d back in Available as failed", pending, failed)
	}

	if n, err := store.VideoStore().ReconnectOrphanedVideos(); err != nil {
		logger.Pl.E("Failed to reconnect videos to their channel URLs: %v", err)
	} else if n > 0 {
		logger.Pl.I("Reconnected %d video(s) whose channel URL had been replaced to the channel URL they match", n)
	}

	// Uploaded thumbnails are kept while a video's Download As options use them, as later Metarr runs need them.
	if overrides, err := store.VideoStore().ThumbnailOverrides(); err != nil {
		logger.Pl.E("Failed to check for unused uploaded thumbnails: %v", err)
	} else {
		file.RemoveUnusedThumbnails(overrides)
	}

	return store, db, progControl, err
}
