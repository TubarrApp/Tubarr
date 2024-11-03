package main

import (
	build "Tubarr/internal/command/builder"
	execute "Tubarr/internal/command/execute"
	"Tubarr/internal/config"
	keys "Tubarr/internal/domain/keys"
	browser "Tubarr/internal/utils/browser"
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

var startTime time.Time

func init() {
	startTime = time.Now()
	logging.PrintI("Tubarr started at: %v", startTime.Format("2006-01-02 15:04:05.00 MST"))
}

// main is the program entrypoint (duh!)
func main() {
	if err := config.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Println()
		os.Exit(1)
	}

	if !viper.GetBool("execute") {
		fmt.Println()
		return // Exit early if not meant to execute
	}

	var (
		directory,
		vDir string
	)

	if config.IsSet(keys.VideoDir) {
		vDir = config.GetString(keys.VideoDir)
		if !strings.HasSuffix(directory, "/") {
			vDir += "/"
			directory = vDir
		}
		// Create directory if it doesn't exist
		if err := os.MkdirAll(directory, 0755); err != nil {
			fmt.Println("Failed to create directory structure:", err)
			fmt.Println()
			os.Exit(1)
		}
	} else {
		logging.Print("No video directory sent in. Skipping logging")
	}

	if directory != "" {
		if err := logging.SetupLogging(directory); err != nil {
			fmt.Printf("\n\nNotice: Log file was not created\nReason: %s\n\n", err)
		}
	} else {
		fmt.Println("Directory and file strings were entered empty. Exiting...")
		fmt.Println()
		os.Exit(1)
	}

	if err := process(); err != nil {
		logging.PrintE(0, err.Error())
		os.Exit(1)
	}

	endTime := time.Now()
	logging.PrintI("Tubarr finished at: %v", endTime.Format("2006-01-02 15:04:05.00 MST"))
	logging.PrintI("Time elapsed: %v", endTime.Sub(startTime))
}

// process begins the main Tubarr program
func process() error {
	if !config.IsSet(keys.ChannelCheckNew) {
		return fmt.Errorf("no channels configured to check")
	}

	urls := browser.GetNewReleases()

	if len(urls) == 0 {
		logging.PrintI("No new URLs received from crawl")
		return nil
	}

	dlFiles, err := execute.DownloadVideos(urls)
	if err != nil {
		return fmt.Errorf("error downloading new videos: %w", err)
	}

	if config.IsSet(keys.MetarrPreset) && len(dlFiles) > 0 {
		mcb := build.NewMetarrCommandBuilder()
		commands, err := mcb.MakeMetarrCommands(dlFiles)
		if err != nil {
			return fmt.Errorf("failed to build metarr commands: %w", err)
		}

		if err := execute.RunMetarr(commands); err != nil {
			return fmt.Errorf("failed to run metarr commands: %w", err)
		}
	}

	return nil
}
