package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tubarr/internal/cfg"
	"tubarr/internal/domain/keys"
	"tubarr/internal/process"
	"tubarr/internal/utils/logging"
)

// main is the main entrypoint of the program (duh!)
func main() {
	startTime := time.Now()
	store, progControl, err := initializeApplication(startTime)
	if err != nil {
		logging.E(0, "error initializing Tubarr: %v", err)
		return
	}
	logging.I("Tubarr (PID: %d) started at: %v", progControl.ProcessID, startTime.Format("2006-01-02 15:04:05.00 MST"))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGSEGV)
	defer cancel()
	defer cleanup(progControl)

	// Start heatbeat
	go startHeartbeat(progControl, ctx)

	// Cobra/Viper commands
	if err := cfg.InitCommands(store, ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	// Execute Cobra/Viper
	if err := cfg.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	// Check channels
	if cfg.GetBool(keys.CheckChannels) {
		if err := process.CheckChannels(store, ctx); err != nil {
			logging.E(0, "Encountered errors while checking channels: %v\n", err)
			return
		}
	}

	endTime := time.Now()
	logging.I("Tubarr finished at: %v\n\nTime elapsed: %.2f seconds",
		endTime.Format("2006-01-02 15:04:05.00 MST"),
		endTime.Sub(startTime).Seconds())
	fmt.Println()
}
