package database

import (
	"fmt"
	"os"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/utils/logging"

	"github.com/Masterminds/squirrel"
)

// StartTubarr sets Tubarr fields in the database
func StartTubarr() (pid int, err error) {

	if err := resetStaleProcess(); err != nil {
		return pid, fmt.Errorf("failed to correct stale process")
	}

	id, running := checkProgRunning()
	if running {
		return pid, fmt.Errorf("process already running (PID: %d)", id)
	}

	logging.I("Tubarr running status: %v", running)

	pid = os.Getpid()
	host, _ := os.Hostname()

	now := time.Now()
	query := squirrel.
		Update(consts.DBProgram).
		Set(consts.QProgRunning, true).
		Set(consts.QProgPID, pid).
		Set(consts.QProgStartedAt, now).
		Set(consts.QProgHeartbeat, now).
		Set(consts.QProgHost, host).
		Where(squirrel.Eq{consts.QProgID: 1}).
		RunWith(db)

	if _, err := query.Exec(); err != nil {
		return pid, err
	}
	return pid, nil
}

// QuitTubarr sets the program exit fields, ready for next run
func QuitTubarr() error {
	if id, running := checkProgRunning(); !running {
		return fmt.Errorf("tubarr is not marked as running. Process %d still active?", id)
	}

	now := time.Now()
	host, _ := os.Hostname()

	query := squirrel.
		Update(consts.DBProgram).
		Set(consts.QProgRunning, false).
		Set(consts.QProgPID, 0).
		Set(consts.QProgHeartbeat, now).
		Set(consts.QProgHost, host).
		Where(squirrel.Eq{consts.QProgID: 1}).
		RunWith(db)

	if _, err := query.Exec(); err != nil {
		return err
	}
	fmt.Println()
	logging.I("Quitting Tubarr...\n")
	return nil
}

// UpdateHeartbeat updates the program heartbeat
func UpdateHeartbeat() error {
	query := squirrel.
		Update(consts.DBProgram).
		Set(consts.QProgHeartbeat, time.Now()).
		Where(squirrel.Eq{consts.QProgID: 1}).
		RunWith(db)

	if _, err := query.Exec(); err != nil {
		return err
	}
	return nil
}

// Private ////////////////////////////////////////////////////////////////////////////////////////////

// checkProgRunning checks if the program is already running.
func checkProgRunning() (int, bool) {
	var (
		running bool
		pid     int
	)

	query := squirrel.
		Select(consts.QProgRunning, consts.QProgPID).
		From(consts.DBProgram).
		Where(squirrel.Eq{consts.QProgID: 1}).
		RunWith(db)

	if err := query.QueryRow().Scan(&running, &pid); err != nil {
		logging.E(0, "Failed to query program running row: %v", err)
		return pid, false
	}

	return pid, running
}

// resetStaleProcess is useful when there are powercuts, etc.
func resetStaleProcess() error {
	var lastHeartbeat time.Time

	query := squirrel.
		Select(consts.QProgHeartbeat).
		From(consts.DBProgram).
		Where(squirrel.Eq{consts.QProgID: 1}).
		RunWith(db)

	if err := query.QueryRow().Scan(&lastHeartbeat); err != nil {
		return err
	}

	if time.Since(lastHeartbeat) > 2*time.Minute {

		logging.I("Detected stale process, resetting state...")

		resetQuery := squirrel.
			Update(consts.DBProgram).
			Set(consts.QProgRunning, false).
			Set(consts.QProgPID, 0).
			Where(squirrel.Eq{consts.QProgID: 1}).
			RunWith(db)

		if _, err := resetQuery.Exec(); err != nil {
			return err
		}
	}
	return nil // Return normal
}
