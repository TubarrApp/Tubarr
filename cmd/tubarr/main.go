// Package main is the entrypoint of Tubarr.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
	"tubarr/internal/abstractions"
	"tubarr/internal/app"
	"tubarr/internal/cfg"
	"tubarr/internal/domain/keys"
	"tubarr/internal/server"
	"tubarr/internal/utils/logging"
	"tubarr/internal/utils/times"
)

// main is the main entrypoint of the program (duh!).
func main() {
	startTime := time.Now()
	store, database, progControl, err := initializeApplication()
	if err != nil {
		logging.E("error initializing Tubarr: %v", err)
		return
	}

	logging.I("Tubarr (PID: %d) started at: %v",
		progControl.ProcessID, startTime.Format("2006-01-02 15:04:05.00 MST"))

	// create cancellable context for shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	// heartbeat shutdown channel
	heartbeatDone := make(chan struct{})

	// run heartbeat goroutine
	go func() {
		startHeartbeat(ctx, progControl)
		close(heartbeatDone)
	}()

	// ---- INIT COMMANDS ----
	if err := cfg.InitCommands(ctx, store); err != nil {
		logging.E("Error: %v", err)
		cancel()
		<-heartbeatDone
		cleanup(progControl, startTime)
		return
	}

	// ---- RUN PROGRAM ----
	runErr := func() error {
		if err := cfg.Execute(); err != nil {
			return err
		}

		if abstractions.IsSet(keys.RunWebInterface) {
			return server.StartServer(ctx, store, database)
		}

		if abstractions.GetBool(keys.TerminalRunDefaultBehavior) {
			if err := times.StartupWait(ctx); err != nil {
				return err
			}
			return app.CheckChannels(ctx, store)
		}
		return nil
	}()

	// ---- SHUTDOWN ----
	cancel()        // stop goroutines
	<-heartbeatDone // wait for heartbeat to flush DB state
	cleanup(progControl, startTime)

	if runErr != nil {
		logging.E("Error: %v", runErr)
	}
}
