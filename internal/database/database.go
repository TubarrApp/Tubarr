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

	// dbParams are applied to every pooled connection (foreign_keys, busy_timeout and synchronous are per-connection).
	dbParams = "?_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL&_synchronous=NORMAL"
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
	d.DB, err = sql.Open(dbDriver, paths.DBFilePath+dbParams)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at path %q: %w", paths.DBFilePath, err)
	}
	if err := d.DB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to open database at path %q: %w", paths.DBFilePath, err)
	}

	if err := d.initTables(); err != nil {
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}
	d.reportForeignKeyViolations()
	return d, nil
}

// reportForeignKeyViolations logs rows left pointing at deleted parents (e.g. from before foreign keys were enforced).
func (d *Database) reportForeignKeyViolations() {
	rows, err := d.DB.Query(`PRAGMA foreign_key_check;`)
	if err != nil {
		logger.Pl.W("Failed to check database foreign keys: %v", err)
		return
	}
	defer func() {
		if err := rows.Close(); err != nil {
			logger.Pl.W("Failed to close database foreign key check: %v", err)
		}
	}()

	counts := make(map[string]int)
	for rows.Next() {
		var (
			table, parent string
			rowID, fkID   sql.NullInt64
		)
		if err := rows.Scan(&table, &rowID, &parent, &fkID); err != nil {
			logger.Pl.W("Failed to read database foreign key check: %v", err)
			return
		}
		counts[table+" → "+parent]++
	}
	for tables, n := range counts {
		logger.Pl.W("Database has %d row(s) in %s pointing at missing parent rows", n, tables)
	}
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

	if err := addColumnIfMissing(tx, "videos", "metarr_status", "TEXT DEFAULT 'done'"); err != nil {
		return err
	}

	if err := addColumnIfMissing(tx, "videos", "duration", "INTEGER DEFAULT 0"); err != nil {
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

	if err := initSettingsTable(tx); err != nil {
		return err
	}

	if err := initWebSessionsTable(tx); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}
