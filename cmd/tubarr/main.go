// Package main is the entrypoint of Tubarr.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/cfg"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/domain/vars"
	"tubarr/internal/file"
	"tubarr/internal/server"
	"tubarr/internal/times"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/logging"
)

// init runs before the program begins.
func init() {
	if err := paths.InitProgFilesDirs(); err != nil {
		fmt.Printf("Tubarr exiting with error: %v\n", err)
		return
	}
}

// main is the main entrypoint of the program (duh!).
func main() {
	startTime := time.Now()

	fmt.Printf("\nMain Tubarr file/dir locations:\n\nDatabase: %s\nLog file: %s\n\n",
		paths.DBFilePath, paths.TubarrLogFilePath)

	// Setup Tubarr logging
	logConfig := logging.LoggingConfig{
		LogFilePath: paths.TubarrLogFilePath,
		MaxSizeMB:   1,
		MaxBackups:  3,
		Console:     os.Stdout,
		Program:     "Tubarr",
	}

	pl, err := logging.SetupLogging(logConfig)
	if err != nil {
		fmt.Printf("Tubarr exiting with error: %v\n", err)
		return
	}
	logger.Pl = pl

	// Initialize application (DB, stores, etc)
	store, database, progControl, err := initializeApplication()
	if err != nil {
		logger.Pl.E("error initializing Tubarr: %v", err)
		return
	}

	// Load in Metarr logs
	vars.MetarrLogs = file.LoadMetarrLogs()

	logger.Pl.I("Tubarr (PID: %d) started at: %v",
		progControl.ProcessID, startTime.Format("2006-01-02 15:04:05.00 MST"))

	// create cancellable context for shutdown
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)

	// heartbeat shutdown channel
	heartbeatDone := make(chan struct{})

	// run heartbeat goroutine
	go func() {
		startHeartbeat(ctx, progControl)
		close(heartbeatDone)
	}()

	// ---- INIT COMMANDS ----
	if err := cfg.InitCommands(ctx, store); err != nil {
		logger.Pl.E("Error: %v", err)
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
		logger.Pl.E("Error: %v", runErr)
	}
}
