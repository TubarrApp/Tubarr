// Package database sets up/opens the program database.
package database

import (
	"database/sql"
	"fmt"
	"tubarr/internal/domain/setup"
	"tubarr/internal/utils/logging"

	_ "github.com/mattn/go-sqlite3"
)

type DBControl struct {
	DB *sql.DB
}

// InitDB returns a new DB control instance.
//
// Can initiate or return database, and perform main program operations.
func InitDB() (dbc *DBControl, err error) {
	var dc DBControl

	dc.DB, err = sql.Open("sqlite3", setup.DBFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at path %q: %w", setup.DBFilePath, err)
	}

	if err := dc.initTables(); err != nil {
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}
	return &dc, nil
}

// initTables initializes the SQL tables.
func (dc *DBControl) initTables() error {
	var (
		committed bool
	)

	tx, err := dc.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				logging.E(0, "transaction rollback failed: %v", err)
			}
		}
	}()

	if err := initProgramTable(tx); err != nil {
		return err
	}

	if err := initChannelsTable(tx); err != nil {
		return err
	}

	if err := initVideosTable(tx); err != nil {
		return err
	}

	if err := initDownloadsTable(tx); err != nil {
		return err
	}

	if err := initNotifyTable(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err == nil { // err IS nil
		committed = false
	}
	return err // nil unless error
}
