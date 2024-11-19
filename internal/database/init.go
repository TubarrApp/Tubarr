package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

func init() {
	const (
		tDir  = ".tubarr"
		tFile = "tubarr.db"
	)

	dir, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("failed to get home directory")
	}

	dir = filepath.Join(dir, tDir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Fatalf("failed to make directories: %v", err)
	}

	path := filepath.Join(dir, tFile)

	db, err = sql.Open("sqlite3", path)
	if err != nil {
		log.Fatalf("failed to open database at path '%s': %v", path, err)
	}

	if err := initTables(db); err != nil {
		log.Fatalf("failed to initialize tables: %v", err)
	}
}

// GrabDB returns the database
func GrabDB() *sql.DB {
	return db
}

// initTables initializes the SQL tables
func initTables(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

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
