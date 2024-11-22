package database

import (
	"database/sql"
	"fmt"
	"tubarr/internal/domain/setup"

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
		return nil, fmt.Errorf("failed to open database at path %q: %v", setup.DBFilePath, err)
	}

	if err := dc.initTables(); err != nil {
		return nil, fmt.Errorf("failed to initialize tables: %v", err)
	}
	return &dc, nil
}

// initTables initializes the SQL tables.
func (dc *DBControl) initTables() error {
	tx, err := dc.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := initProgramTable(tx); err != nil {
		return err
	}

	if err := initChannelsTable(tx); err != nil {
		return err
	}

	if err := initVideosTable(tx); err != nil {
		return err
	}

	if err := initNotifyTable(tx); err != nil {
		return err
	}

	return tx.Commit()
}
