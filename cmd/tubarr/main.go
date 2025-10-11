package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tubarr/internal/app"
	"tubarr/internal/cfg"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/utils/logging"

	"github.com/spf13/viper"
)

const (
	timeRemainingMsg = consts.ColorCyan + "Time remaining:" + consts.ColorReset
)

// main is the main entrypoint of the program (duh!)
func main() {
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
	if viper.GetBool(keys.CheckChannels) {
		// Immediate run if skipping wait
		if viper.GetBool(keys.SkipWait) {
			logging.W("Skipping wait period, running Tubarr immediately. You may encounter bot detection on some platforms if requests come at predictable intervals.")
			if err := app.CheckChannels(ctx, store); err != nil {
				logging.E("Encountered errors while checking channels: %v", err)
			}
		} else {

			// Add random jitter on startup (0-30 minutes)
			maxJitter := 30 * time.Minute
			jitter := time.Duration(rand.Intn(int(maxJitter)))

			logging.I("Waiting %v before channel check (helps hide from bot detection). To skip startup jitter, use:\n\ntubarr -s\n", jitter.Round(time.Second))
			// Countdown display
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()

			endTime := time.Now().Add(jitter)

			go func() {
				for {
					select {
					case <-ticker.C:
						remaining := time.Until(endTime)
						if remaining <= 0 {
							fmt.Print(consts.ClearLine)
							return
						}
						m := int(remaining.Minutes())
						s := int(remaining.Seconds()) % 60
						fmt.Print(consts.ClearLine)
						fmt.Printf("\r%s %dm%ds", timeRemainingMsg, m, s)
					case <-ctx.Done():
						fmt.Print(consts.ClearLine)
						return
					}
				}
			}()

			// Wait for the jitter delay
			select {
			case <-time.After(jitter):
				fmt.Print(consts.ClearLine)
				if err := app.CheckChannels(ctx, store); err != nil {
					logging.E("Encountered errors while checking channels: %v", err)
				}
			case <-ctx.Done():
				logging.I("Exiting before channel check")
				return
			}
		}
	}
}
