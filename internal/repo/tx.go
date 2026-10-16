package repo

import (
	"database/sql"
	"errors"
	"fmt"
	"tubarr/internal/domain/logger"
)

// querier runs queries on the database, or on a transaction.
type querier interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// inTx runs fn in a transaction on db, committing if fn returns nil and rolling back otherwise (including on panic).
func inTx(db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer rollback(tx)

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// rollback rolls back tx unless it's been committed, for deferring straight after Begin.
func rollback(tx *sql.Tx) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		logger.Pl.E("Failed to roll back transaction: %v", err)
	}
}
