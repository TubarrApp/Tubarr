package database

import (
	"database/sql"
	"fmt"
	"tubarr/internal/domain/setup"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

// InitDB opens the database and creates/initializes tables.
func InitDB() (err error) {
	db, err = sql.Open("sqlite3", setup.DBFilePath)
	if err != nil {
		return fmt.Errorf("failed to open database at path %q: %v", setup.DBFilePath, err)
	}

	if err := initTables(db); err != nil {
		return fmt.Errorf("failed to initialize tables: %v", err)
	}
	return nil
}

// GrabDB returns the database
func GrabDB() *sql.DB {
	return db
}

// initTables initializes the SQL tables.
func initTables(db *sql.DB) error {
	tx, err := db.Begin()
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
