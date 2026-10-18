// Package main is the entrypoint of Tubarr.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"
	"tubarr/internal/app"
	"tubarr/internal/blocking"
	"tubarr/internal/cfg"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/domain/vars"
	"tubarr/internal/file"
	"tubarr/internal/server"
	"tubarr/internal/times"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/buildinfo"
	"github.com/TubarrApp/gocommon/logging"
)

// init runs before the program begins.
func init() {
	if err := paths.InitProgFilesDirs(); err != nil {
		fmt.Printf("Tubarr exiting with error: %v\n", err)
		os.Exit(1)
	}
}

// isHelp reports whether the arguments only ask for help, which doesn't take the single-instance lock.
func isHelp(args []string) bool {
	return (len(args) > 0 && args[0] == "help") || slices.Contains(args, "-h") || slices.Contains(args, "--help")
}

// main is the main entrypoint of the program (duh!).
func main() {
	startTime := time.Now()

	fmt.Printf("\nMain Tubarr file/dir locations:\n\nDatabase: %s\nLog file: %s\n\n",
		paths.DBFilePath, paths.TubarrLogFilePath)

	// Setup Tubarr logging.
	logConfig := logging.Config{
		LogFilePath: paths.TubarrLogFilePath,
		MaxSizeMB:   1,
		MaxBackups:  3,
		Console:     os.Stdout,
		Program:     "Tubarr",
	}

	pl, err := logging.SetupLogging(logConfig)
	if err != nil {
		fmt.Printf("Tubarr exiting with error: %v\n", err)
		os.Exit(1)
	}
	logger.Pl = pl

	// Initialize application (DB, stores, etc).
	help := isHelp(os.Args[1:])
	store, db, progControl, err := initializeApplication(!help)
	if err != nil {
		logger.Pl.E("Error initializing Tubarr: %v", err)
		os.Exit(1)
	}

	// Load in Metarr logs.
	vars.MetarrLogs = file.LoadMetarrLogs()

	// Load blocked domains from database.
	if err := blocking.LoadBlockedDomains(db); err != nil {
		logger.Pl.W("Failed to load blocked domains: %v", err)
	}

	logger.Pl.I("Tubarr binary %s", buildinfo.Get())
	logger.Pl.I("Tubarr (PID: %d) started at: %v",
		progControl.ProcessID, startTime.Format("2006-01-02 15:04:05.00 MST"))

	// create cancellable context for shutdown.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)

	// heartbeat shutdown channel.
	heartbeatDone := make(chan struct{})

	// run heartbeat goroutine, unless only showing help.
	go func() {
		if !help {
			startHeartbeat(ctx, progControl)
		}
		close(heartbeatDone)
	}()

	// ---- INIT COMMANDS (e.g. --web, --debug) ----
	if err := cfg.InitCommands(ctx, store); err != nil {
		logger.Pl.E("Error: %v", err)
		cancel()
		<-heartbeatDone
		cleanup(progControl, startTime)
		os.Exit(1)
	}

	// ---- RUN PROGRAM ----
	runErr := func() error {
		if err := cfg.Execute(); err != nil {
			return err
		}

		if abstractions.GetBool(keys.RunWebInterface) {
			return server.StartServer(ctx, cancel, store, db)
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
	cancel()        // stop goroutines.
	<-heartbeatDone // wait for heartbeat to flush DB state.
	cleanup(progControl, startTime)

	if runErr != nil {
		logger.Pl.E("Error: %v", runErr)
		os.Exit(1)
	}
}
