package repo

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
)

// ProgControl holds a pointer to the sql.DB, and program process ID.
type ProgControl struct {
	DB        *sql.DB
	ProcessID int
}

// NewProgController returns a program controller for updating primary program elements.
//
// For example, starting and exiting the program to ensure only one Tubarr instance runs at a time.
func NewProgController(database *sql.DB) *ProgControl {
	return &ProgControl{
		DB: database,
	}
}

// StartTubarr sets Tubarr fields in the database.
func (pc *ProgControl) StartTubarr() (pid int, err error) {
	// Check running or stale state.
	id, running, err := pc.checkProgRunning()
	if err != nil {
		return 0, err
	}
	if running {
		reset, err := pc.resetStaleProcess()
		if err != nil {
			return 0, fmt.Errorf("failure: could not correct stale process, unexpected error: %w", err)
		}
		if !reset {
			return 0, fmt.Errorf("another instance is already running (PID: %d)", id)
		}
	}

	pid = os.Getpid()
	host, err := os.Hostname()
	if err != nil {
		logger.Pl.E("Failed to get device hostname: %v", err)
	}

	now := time.Now()
	query := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ?, %s = ?, %s = ?, %s = ? WHERE %s = 1",
		consts.DBProgram,
		consts.QProgRunning,
		consts.QProgPID,
		consts.QProgStartedAt,
		consts.QProgHeartbeat,
		consts.QProgHost,
		consts.QProgID,
	)

	if _, err := pc.DB.Exec(
		query,
		true,
		pid,
		now,
		now,
		host,
	); err != nil {
		return pid, fmt.Errorf("failure: %w", err)
	}

	return pid, nil
}

// QuitTubarr sets the program exit fields, ready for next run.
func (pc *ProgControl) QuitTubarr(startTime time.Time) error {
	id, running, err := pc.checkProgRunning()
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("tubarr is not marked as running. Process %d still active?", id)
	}

	now := time.Now()
	host, err := os.Hostname()
	if err != nil {
		logger.Pl.E("Failed to get device hostname: %v", err)
	}

	query := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ?, %s = ?, %s = ? WHERE %s = 1",
		consts.DBProgram,
		consts.QProgRunning,
		consts.QProgPID,
		consts.QProgHeartbeat,
		consts.QProgHost,
		consts.QProgID,
	)

	if _, err := pc.DB.Exec(
		query,
		false,
		0,
		now,
		host,
	); err != nil {
		return err
	}

	logger.Pl.I("Tubarr finished: %v\n\nTime elapsed: %.2f seconds\n",
		now.Local().Format("2006-01-02 15:04:05.00 MST"),
		now.Sub(startTime).Seconds())
	return nil
}

// UpdateHeartbeat updates the program heartbeat.
//
// This function is crucial for ensuring things like powercuts don't
// permanently lock the user out of the database.
func (pc *ProgControl) UpdateHeartbeat() error {
	query := fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE %s = 1",
		consts.DBProgram,
		consts.QProgHeartbeat,
		consts.QProgID,
	)

	_, err := pc.DB.Exec(query, time.Now())
	if err != nil {
		return err
	}

	return nil
}

// ******************************** Private ***************************************************************************************

// checkProgRunning checks if the program is already running.
func (pc *ProgControl) checkProgRunning() (int, bool, error) {
	var (
		running bool
		pid     sql.NullInt64
	)

	query := fmt.Sprintf(
		"SELECT %s, %s FROM %s WHERE %s = 1",
		consts.QProgRunning,
		consts.QProgPID,
		consts.DBProgram,
		consts.QProgID,
	)

	err := pc.DB.QueryRow(query).Scan(&running, &pid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("failed to query program running row: %w", err)
	}

	// Extract the int value, defaulting to 0 if NULL.
	pidValue := int(0)
	if pid.Valid {
		pidValue = int(pid.Int64)
	}

	return pidValue, running, nil
}

// resetStaleProcess is useful when there are powercuts, etc.
func (pc *ProgControl) resetStaleProcess() (reset bool, err error) {
	var lastHeartbeat time.Time

	query := fmt.Sprintf(
		"SELECT %s FROM %s WHERE %s = 1",
		consts.QProgHeartbeat,
		consts.DBProgram,
		consts.QProgID,
	)

	if err := pc.DB.QueryRow(query).Scan(&lastHeartbeat); err != nil {
		return false, err
	}

	if time.Since(lastHeartbeat) > 2*time.Minute {

		logger.Pl.I("Detected stale process, resetting state...")

		resetQuery := fmt.Sprintf(
			"UPDATE %s SET %s = ?, %s = ? WHERE %s = 1",
			consts.DBProgram,
			consts.QProgRunning,
			consts.QProgPID,
			consts.QProgID,
		)

		if _, err := pc.DB.Exec(resetQuery, false, 0); err != nil {
			return false, err
		}
		return true, nil // Reset, no error.
	}
	return false, nil // Not reset, no error.
}
