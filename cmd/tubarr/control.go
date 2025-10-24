package main

import (
	"context"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/repo"
	"tubarr/internal/utils/logging"
)

// startHeartbeat starts the program heartbeat.
//
// Mainly useful for preventing DB lockouts.
func startHeartbeat(ctx context.Context, progControl *repo.ProgControl) {
	ticker := time.NewTicker(consts.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := progControl.UpdateHeartbeat(); err != nil {
				logging.E("Failed to update heartbeat for process ID %d: %v", progControl.ProcessID, err)
			}
		}
	}
}

// cleanup safely quits the program.
func cleanup(progControl *repo.ProgControl, startTime time.Time) {
	defer func() {
		r := recover() // grab panic condition
		if r != nil {
			logging.E("Panic occurred: %v", r)
		}

		if err := progControl.QuitTubarr(startTime); err != nil {
			logging.E("!!! Failed to mark Tubarr as exited, won't run again until heartbeat goes stale (2 minutes)")
		}

		if r != nil {
			panic(r)
		}
	}()
}
