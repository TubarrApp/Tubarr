package main

import (
	"fmt"
	"os"
	"time"
	"tubarr/internal/cfg"
	"tubarr/internal/database"
	"tubarr/internal/process"
	"tubarr/internal/repo"
	"tubarr/internal/utils/logging"
)

var startTime time.Time

func init() {
	startTime = time.Now()
	logging.I("Tubarr started at: %v", startTime.Format("2006-01-02 15:04:05.00 MST"))
}

// main is the program entrypoint (duh!)
func main() {

	// Initialize database and channel store
	db := database.GrabDB()
	store := repo.InitStores(db)

	// Initialize commands with dependencies
	cfg.InitCommands(store)

	// Execute root command
	if err := cfg.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if cfg.GetBool("check_channels") {
		if err := process.CheckChannels(store); err != nil {
			fmt.Fprintf(os.Stderr, "Error checking channels: %v\n", err)
			os.Exit(1)
		}
	}

	endTime := time.Now()
	logging.I("metarr finished at: %v", endTime.Format("2006-01-02 15:04:05.00 MST"))
	logging.I("Time elapsed: %.2f seconds", endTime.Sub(startTime).Seconds())
	fmt.Println()
}
