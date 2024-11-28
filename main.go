package main

import (
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
	"tubarr/internal/cfg"
	"tubarr/internal/database"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/setup"
	"tubarr/internal/process"
	"tubarr/internal/repo"
	"tubarr/internal/utils/benchmark"
	"tubarr/internal/utils/logging"
)

var (
	store     *repo.Store
	dbc       *database.DBControl
	pc        *repo.ProgControl
	startTime time.Time
	err       error
)

func init() {
	// Get start time ASAP
	startTime = time.Now()

	// Get directory of main.go (helpful for benchmarking file save locations)
	_, mainGoPath, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintf(os.Stderr, "Error getting current working directory. Got: %v\n", mainGoPath)
		os.Exit(1)
	}
	benchmark.InjectMainWorkDir(mainGoPath)

	// Setup files/dirs
	if err := setup.InitCfgFilesDirs(startTime.Format("2006-01-02 15:04:05.00 MST")); err != nil {
		fmt.Printf("Tubarr exiting: %v\n", err)
		os.Exit(0)
	}

	fmt.Printf("\nMain Tubarr file/dir locations:\n\nDatabase: %s\nLog file: %s\n\n",
		setup.DBFilePath, setup.LogFilePath)

	// Database & stores
	dbc, err = database.InitDB()
	if err != nil {
		fmt.Printf("Tubarr exiting: %v\n", err)
		os.Exit(0)
	}
	store = repo.InitStores(dbc.DB)

	// Start controller
	pc = repo.NewProgController(dbc.DB)
	id, err := pc.StartTubarr()
	if err != nil {
		if strings.HasPrefix(err.Error(), "failure:") {
			logging.E(0, "DB %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Tubarr exiting: %v\n", err)
		os.Exit(0)
	}

	// Setup logging
	if err := logging.SetupLogging(setup.CfgDir); err != nil {
		fmt.Printf("could not set up logging, proceeding without: %v", err)
	}

	// Print start time after logging is setup
	logging.I("Tubarr (PID: %d) started at: %v", id, startTime.Format("2006-01-02 15:04:05.00 MST"))
}

// main is the main entrypoint of the program (duh!)
func main() {

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGSEGV)

	// Signal handler
	go func() {
		<-sigChan
		if err := pc.QuitTubarr(); err != nil {
			logging.E(0, "!!! Failed to mark Tubarr as exited, won't run again until heartbeat goes stale (2 minutes)")
		}
		os.Exit(1)
	}()

	// Panic handler
	defer func() {
		if r := recover(); r != nil {
			if err := pc.QuitTubarr(); err != nil {
				logging.E(0, "!!! Failed to mark Tubarr as exited, won't run again until heartbeat goes stale (2 minutes)")
			}
			panic(r)
		}
	}()

	// Clean exit handler
	defer func() {
		if err := pc.QuitTubarr(); err != nil {
			logging.E(0, "!!! Failed to mark Tubarr as exited, won't run again until heartbeat goes stale (2 minutes)")
		}
	}()

	// Heartbeat
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			if err := pc.UpdateHeartbeat(); err != nil {
				logging.E(0, "Failed to update heartbeat: %v", err)
			}
		}
	}()

	// Cobra/Viper commands
	if err := cfg.InitCommands(store); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return
	}

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
	logging.I("Tubarr finished at: %v\n\nTime elapsed: %.2f seconds",
		endTime.Format("2006-01-02 15:04:05.00 MST"),
		endTime.Sub(startTime).Seconds())
	fmt.Println()
}
