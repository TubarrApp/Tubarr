// Package database sets up/opens the program database.
package database

import (
	"database/sql"
	"fmt"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"

	// Package sqlite3 provides interface to SQLite3 databases.
	_ "github.com/mattn/go-sqlite3"
)

const (
	dbDriver = "sqlite3"
)

// Database holds the global database instance for Tubarr.
type Database struct {
	DB *sql.DB
}

// InitDB returns a new DB control instance.
//
// Can initiate or return database, and perform main program operations.
func InitDB() (d *Database, err error) {
	d = new(Database)
	d.DB, err = sql.Open(dbDriver, paths.DBFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at path %q: %w", paths.DBFilePath, err)
	}

	// Enable foreign keys
	if _, err := d.DB.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Enable Write-Ahead Logging for concurrent access
	if _, err := d.DB.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Allow SQLite to wait for locks (in milliseconds)
	if _, err := d.DB.Exec(`PRAGMA busy_timeout = 5000;`); err != nil {
		return nil, fmt.Errorf("failed to set busy_timeout: %w", err)
	}

	// Slightly reduce fsync frequency for faster writes
	if _, err := d.DB.Exec(`PRAGMA synchronous = NORMAL;`); err != nil {
		return nil, fmt.Errorf("failed to set synchronous mode: %w", err)
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
		if p := recover(); p != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Panic rollback failed for table creation: %v", rbErr)
			}
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("transaction rollback failed after original error %v: %v", err, rbErr)
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

	if err := initBlockedDomainsTable(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
