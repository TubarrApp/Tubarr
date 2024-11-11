package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"tubarr/internal/cfg"
	build "tubarr/internal/command/builder"
	execute "tubarr/internal/command/execute"
	keys "tubarr/internal/domain/keys"
	"tubarr/internal/process"
	browser "tubarr/internal/utils/browser"
	fsWrite "tubarr/internal/utils/fs/write"
	logging "tubarr/internal/utils/logging"

	"github.com/spf13/viper"
)

var startTime time.Time

func init() {
	startTime = time.Now()
	logging.I("tubarr started at: %v", startTime.Format("2006-01-02 15:04:05.00 MST"))
}

// main is the program entrypoint (duh!)
func main() {
	if err := cfg.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Println()
		os.Exit(1)
	}

	if !viper.GetBool("execute") {
		fmt.Println()
		return // Exit early if not meant to execute
	}

	var (
		vDir, jDir string
	)

	// Video directory setup
	if cfg.IsSet(keys.VideoDir) {
		vDir = cfg.GetString(keys.VideoDir)
		if !strings.HasSuffix(vDir, "/") {
			vDir += "/"
		}
		// Create directory if it doesn't exist
		if err := os.MkdirAll(vDir, 0755); err != nil {
			fmt.Println("Failed to create directory structure:", err)
			fmt.Println()
			os.Exit(1)
		}
	} else {
		fmt.Println("No video directory sent in. Skipping logging")
	}

	// Json directory setup
	if cfg.IsSet(keys.JsonDir) {
		jDir = cfg.GetString(keys.JsonDir)
		if !strings.HasSuffix(jDir, "/") {
			jDir += "/"
		}
		// Create directory if it doesn't exist
		if err := os.MkdirAll(jDir, 0755); err != nil {
			fmt.Println("Failed to create directory structure:", err)
			fmt.Println()
			os.Exit(1)
		}
	} else {
		fmt.Println("No video directory sent in. Skipping logging")
	}

	// Setup logging
	if vDir != "" {
		if err := logging.SetupLogging(vDir); err != nil {
			fmt.Printf("\n\nNotice: Log file was not created\nReason: %s\n\n", err)
		}
	} else if jDir != "" {
		if err := logging.SetupLogging(jDir); err != nil {
			fmt.Printf("\n\nNotice: Log file was not created\nReason: %s\n\n", err)
		}
	} else {
		fmt.Println("Directory and file strings were entered empty. Exiting...")
		fmt.Println()
		os.Exit(1)
	}

	// Begin processing
	if err := initProcess(vDir, jDir); err != nil {
		logging.E(0, err.Error())
		os.Exit(1)
	}

	endTime := time.Now()
	logging.I("tubarr finished at: %v", endTime.Format("2006-01-02 15:04:05.00 MST"))
	logging.I("Time elapsed: %.2f seconds", endTime.Sub(startTime).Seconds())
}

// process begins the main tubarr program
func initProcess(vDir, jDir string) error {
	if !cfg.IsSet(keys.ChannelCheckNew) {
		return fmt.Errorf("no channels configured to check")
	}

	requests := browser.GetNewReleases(vDir, jDir)

	if len(requests) == 0 {
		logging.I("No new requests received from crawl")
		return nil
	}

	var toAppend []string

	// Returns DLs that pass checks, should be furnished with JSON directories and paths
	dls, unwanted, err := process.ProcessMetaDownloads(requests)
	if err != nil {
		return err
	}

	if len(unwanted) > 0 {
		toAppend = append(toAppend, unwanted...)
	}

	if len(dls) == 0 {
		logging.I("Nothing to download, exiting...")
		if len(toAppend) > 0 {
			updateGrabbedUrls(vDir, toAppend)
		}
		return nil
	}

	downloaded, successful, proceed := process.ProcessVideoDownloads(dls)
	if !proceed {
		logging.I("No videos to download, exiting...")
		return nil
	}

	if len(successful) > 0 {
		toAppend = append(toAppend, successful...)
	}

	if len(toAppend) > 0 {
		updateGrabbedUrls(vDir, toAppend)
	}

	if cfg.IsSet(keys.MetarrPreset) && len(downloaded) > 0 {
		mcb := build.NewMetarrCommandBuilder()
		commands, err := mcb.MakeMetarrCommands(downloaded)
		if err != nil {
			return fmt.Errorf("failed to build metarr commands: %w", err)
		}

		if err := execute.RunMetarr(commands); err != nil {
			return fmt.Errorf("failed to run metarr commands: %w", err)
		}
	}

	return nil
}

func updateGrabbedUrls(vDir string, toAppend []string) {

	grabbedURLsPath := filepath.Join(vDir, "grabbed-urls.txt")
	if err := fsWrite.AppendURLsToFile(grabbedURLsPath, toAppend); err != nil {
		logging.E(0, "Failed to update grabbed-urls.txt: %v", err)
	}
}
