package main

import (
	"context"
	"time"

	"tubarr/internal/database/repo"
	"tubarr/internal/utils/logging"
)

// startHeartbeat starts the program heartbeat.
//
// Mainly useful for preventing DB lockouts.
func startHeartbeat(progControl *repo.ProgControl, ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := progControl.UpdateHeartbeat(); err != nil {
				logging.E(0, "Failed to update heartbeat for process ID %d: %v", progControl.ProcessID, err)
			}
		}
	}
}

// cleanup safely quits the program.
func cleanup(progControl *repo.ProgControl) {
	defer func() {
		r := recover() // grab panic condition
		if r != nil {
			logging.E(0, "Panic occurred: %v", r)
		}

		if err := progControl.QuitTubarr(); err != nil {
			logging.E(0, "!!! Failed to mark Tubarr as exited, won't run again until heartbeat goes stale (2 minutes)")
		}

		if r != nil {
			panic(r)
		}
	}()
}
