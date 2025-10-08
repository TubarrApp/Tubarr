package repo

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/utils/logging"

	"github.com/Masterminds/squirrel"
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
	// Check running or stale state
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
		logging.E("Failed to get device hostname: %v", err)
	}

	now := time.Now()
	query := squirrel.
		Update(consts.DBProgram).
		Set(consts.QProgRunning, true).
		Set(consts.QProgPID, pid).
		Set(consts.QProgStartedAt, now).
		Set(consts.QProgHeartbeat, now).
		Set(consts.QProgHost, host).
		Where(squirrel.Eq{consts.QProgID: 1}).
		RunWith(pc.DB)

	if _, err := query.Exec(); err != nil {
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
		logging.E("Failed to get device hostname: %v", err)
	}

	query := squirrel.
		Update(consts.DBProgram).
		Set(consts.QProgRunning, false).
		Set(consts.QProgPID, 0).
		Set(consts.QProgHeartbeat, now).
		Set(consts.QProgHost, host).
		Where(squirrel.Eq{consts.QProgID: 1}).
		RunWith(pc.DB)

	if _, err := query.Exec(); err != nil {
		return err
	}
	logging.I("Tubarr finished: %v\n\nTime elapsed: %.2f seconds\n",
		now.Local().Format("2006-01-02 15:04:05.00 MST"),
		now.Sub(startTime).Seconds())
	return nil
}

// UpdateHeartbeat updates the program heartbeat.
//
// This function is crucial for ensuring things like powercuts don't
// permanently lock the user out of the database.
func (pc *ProgControl) UpdateHeartbeat() error {
	query := squirrel.
		Update(consts.DBProgram).
		Set(consts.QProgHeartbeat, time.Now()).
		Where(squirrel.Eq{consts.QProgID: 1}).
		RunWith(pc.DB)

	if _, err := query.Exec(); err != nil {
		return err
	}
	return nil
}

// ******************************** Private ********************************

// checkProgRunning checks if the program is already running.
func (pc *ProgControl) checkProgRunning() (int, bool, error) {
	var (
		running bool
		pid     sql.NullInt64
	)

	query := squirrel.
		Select(consts.QProgRunning, consts.QProgPID).
		From(consts.DBProgram).
		Where(squirrel.Eq{consts.QProgID: 1}).
		RunWith(pc.DB)

	err := query.QueryRow().Scan(&running, &pid)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("failed to query program running row: %w", err)
	}

	// Extract the int value, defaulting to 0 if NULL
	pidValue := int(0)
	if pid.Valid {
		pidValue = int(pid.Int64)
	}

	return pidValue, running, nil
}

// resetStaleProcess is useful when there are powercuts, etc.
func (pc *ProgControl) resetStaleProcess() (reset bool, err error) {
	var lastHeartbeat time.Time

	query := squirrel.
		Select(consts.QProgHeartbeat).
		From(consts.DBProgram).
		Where(squirrel.Eq{consts.QProgID: 1}).
		RunWith(pc.DB)

	if err := query.QueryRow().Scan(&lastHeartbeat); err != nil {
		return false, err
	}

	if time.Since(lastHeartbeat) > 2*time.Minute {

		logging.I("Detected stale process, resetting state...")

		resetQuery := squirrel.
			Update(consts.DBProgram).
			Set(consts.QProgRunning, false).
			Set(consts.QProgPID, 0).
			Where(squirrel.Eq{consts.QProgID: 1}).
			RunWith(pc.DB)

		if _, err := resetQuery.Exec(); err != nil {
			return false, err
		}
		return true, nil // Reset, no error
	}
	return false, nil // Not reset, no error
}
