package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	"tubarr/internal/cfg"
	"tubarr/internal/database"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/setup"
	"tubarr/internal/process"
	"tubarr/internal/repo"
	"tubarr/internal/utils/logging"
)

var (
	store     *repo.Store
	startTime time.Time
)

func init() {

	// Setup files/dirs
	if err := setup.InitCfgFilesDirs(); err != nil {
		logging.I("Tubarr exiting: %v\n", err)
		os.Exit(0)
	}

	fmt.Printf("\nMain Tubarr file/dir locations:\n\nDatabase: %s\nLog file: %s\n\n", setup.DBFilePath, setup.LogFilePath)

	// Logging
	if err := logging.SetupLogging(); err != nil {
		fmt.Printf("could not set up logging, proceeding without: %v", err)
	}

	// Database & stores
	if err := database.InitDB(); err != nil {
		logging.I("Tubarr exiting: %v\n", err)
		os.Exit(0)
	}
	db := database.GrabDB()
	store = repo.InitStores(db)

	// Start
	pid, err := database.StartTubarr()
	if err != nil {
		logging.I("Tubarr exiting: %v\n", err)
		os.Exit(0)
	}

	startTime = time.Now()
	logging.I("Tubarr (PID: %d) started at: %v", pid, startTime.Format("2006-01-02 15:04:05.00 MST"))
}

// main is the program entrypoint (duh!)
func main() {

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGSEGV)

	// Cleanup functions
	go func() {
		<-sigChan
		if err := database.QuitTubarr(); err != nil {
			logging.E(0, "!!! FAILED TO MARK TUBARR AS EXITED, WILL NOT RUN AGAIN UNLESS DB DELETED")
		}
		os.Exit(1)
	}()

	defer func() {
		if r := recover(); r != nil {
			if err := database.QuitTubarr(); err != nil {
				logging.E(0, "!!! FAILED TO MARK TUBARR AS EXITED, WILL NOT RUN AGAIN UNLESS DB DELETED")
			}
			panic(r)
		}
	}()

	defer func() {
		if err := database.QuitTubarr(); err != nil {
			logging.E(0, "!!! FAILED TO MARK TUBARR AS EXITED, WILL NOT RUN AGAIN UNLESS DB DELETED")
		}
	}()

	// Program heartbeat
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			if err := database.UpdateHeartbeat(); err != nil {
				logging.E(0, "Failed to update heartbeat: %v", err)
			}
		}
	}()

	// Initialize commands with dependencies
	cfg.InitCommands(store)
	if err := cfg.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

	if cfg.GetBool(keys.CheckChannels) {
		if err := process.CheckChannels(store); err != nil {
			logging.E(0, "Encountered errors while checking channels: %v\n", err)
			return
		}
	}

	endTime := time.Now()
	logging.I("metarr finished at: %v", endTime.Format("2006-01-02 15:04:05.00 MST"))
	logging.I("Time elapsed: %.2f seconds", endTime.Sub(startTime).Seconds())
	fmt.Println()
}
