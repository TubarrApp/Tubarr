// Package db sets up/opens the program database.
package db

import (
	"database/sql"
	"fmt"
	"tubarr/internal/domain/setup"
	"tubarr/internal/utils/logging"

	_ "github.com/mattn/go-sqlite3"
)

const (
	dbDriver = "sqlite3"
)

type Database struct {
	DB *sql.DB
}

// InitDB returns a new DB control instance.
//
// Can initiate or return database, and perform main program operations.
func InitDB() (d *Database, err error) {
	d = new(Database)
	d.DB, err = sql.Open(dbDriver, setup.DBFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at path %q: %w", setup.DBFilePath, err)
	}

	// Enable foreign key enforcement
	_, err = d.DB.Exec("PRAGMA foreign_keys = ON;")
	if err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	if err := d.initTables(); err != nil {
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}
	return d, nil
}

// initTables initializes the SQL tables.
func (d *Database) initTables() error {
	tx, err := d.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	defer func() {
		if err != nil {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logging.E("transaction rollback failed: %v", rollbackErr)
			}
		}
	}()

	if err := initProgramTable(tx); err != nil {
		return err
	}

	if err := initChannelsTable(tx); err != nil {
		return err
	}

	if err := initChannelURLsTable(tx); err != nil {
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

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
