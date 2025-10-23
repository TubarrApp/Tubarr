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
	"tubarr/internal/utils/benchmark"
	"tubarr/internal/utils/logging"
	"tubarr/internal/utils/times"
)

// main is the main entrypoint of the program (duh!)
func main() {
	defer func() {
		if r := recover(); r != nil {
			logging.E("Panic recovered: %v", r)
			benchmark.CloseBenchmarking()
			panic(r) // Re-panic after cleanup
		}
	}()

	startTime := time.Now()
	store, progControl, err := initializeApplication()
	if err != nil {
		logging.E("error initializing Tubarr: %v", err)
		return
	}
	logging.I("Tubarr (PID: %d) started at: %v", progControl.ProcessID, startTime.Format("2006-01-02 15:04:05.00 MST"))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGSEGV)
	defer cancel()
	defer cleanup(progControl, startTime)

	// Close benchmarking at the very end
	defer benchmark.CloseBenchmarking()

	// Start heatbeat
	go startHeartbeat(ctx, progControl)

	// Cobra/Viper commands
	if err := cfg.InitCommands(ctx, store); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	// Execute Cobra/Viper
	if err := cfg.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	// Check channels
	if abstractions.GetBool(keys.CheckChannels) {
		// Wait with countdown (or skip if -s flag is set)
		if err := times.StartupWait(ctx); err != nil {
			logging.E("Exiting before startup timer exited")
			return
		}

		if err := app.CheckChannels(ctx, store); err != nil {
			logging.E("Encountered errors while checking channels: %v", err)
		}
	}
}
