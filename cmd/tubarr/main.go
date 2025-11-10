// Package main is the entrypoint of Tubarr.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	"tubarr/internal/abstractions"
	"tubarr/internal/app"
	"tubarr/internal/cfg"
	"tubarr/internal/domain/keys"
	"tubarr/internal/server"
	"tubarr/internal/utils/benchmark"
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
	logging.I("Tubarr (PID: %d) started at: %v\n", progControl.ProcessID, startTime.Format("2006-01-02 15:04:05.00 MST"))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGSEGV)

	// Cleanup runs on ALL exits (normal, panic, signal)
	defer func() {
		benchmark.CloseBenchmarking()
		cleanup(progControl, startTime)
	}()

	// Panic handler
	defer func() {
		if r := recover(); r != nil {
			logging.E("Panic recovered: %v", r)
			panic(r) // Re-panic to preserve stack trace
		}
	}()

	// Start heartbeat with shutdown coordination
	heartbeatDone := make(chan struct{})
	go func() {
		startHeartbeat(ctx, progControl)
		close(heartbeatDone)
	}()
	defer func() {
		cancel()
		<-heartbeatDone
	}()

	// Initialize Viper/Cobra
	if err := cfg.InitCommands(ctx, store); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	if err := cfg.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}
	// Start server or terminal version
	if abstractions.IsSet(keys.RunWebInterface) {
		server.StartServer(store, database)
	} else {
		// Check channels
		if abstractions.GetBool(keys.CheckChannels) {
			if err := times.StartupWait(ctx); err != nil {
				logging.E("Exiting before startup timer exited")
				return
			}

			if err := app.CheckChannels(ctx, store); err != nil {
				logging.E("Encountered errors while checking channels: %v", err)
			}
		}
	}
}
