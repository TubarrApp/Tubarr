package main

import (
	execute "Tubarr/internal/command/execute"
	"Tubarr/internal/config"
	keys "Tubarr/internal/domain/keys"
	"Tubarr/internal/models"
	browser "Tubarr/internal/utils/browser"
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

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

	if config.IsSet(keys.VideoDir) {
		vDir := config.GetString(keys.VideoDir)
		if !strings.HasSuffix(vDir, "/") {
			vDir += "/"
		}

		// Create directory if it doesn't exist
		if err := os.MkdirAll(vDir, 0755); err != nil {
			logging.PrintE(0, "Failed to create directory structure: %v", err)
			os.Exit(1)
		}

		// Open file with create flag
		logFile, err := os.OpenFile(vDir+"tubarr-log.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			logging.PrintE(0, "Encountered an error opening/creating the log file: %v", err)
			os.Exit(1)
		}
		defer logFile.Close()

		if err := logging.SetupLogging(logFile); err != nil {
			logging.PrintE(0, "Encountered error setting up logging: %v", err)
			os.Exit(1)
		}
	} else {
		logging.Print("No video directory sent in. Skipping logging")
	}

	if err := process(); err != nil {
		logging.PrintE(0, err.Error())
		os.Exit(1)
	}
}

// process begins the main Tubarr program:
// Grabs and downloads new releases and
// sends to Metarr for post-processing
func process() error {

	var dlFiles []models.DownloadedFiles
	var err error

	// Get new releases and download videos
	if config.IsSet(keys.ChannelCheckNew) {
		urls := browser.GetNewReleases()
		logging.PrintD(2, "Got URLs: %v", urls)

		dlFiles, err = execute.DownloadVideos(urls)
		if err != nil {
			return fmt.Errorf("error downloading new videos: %w", err)
		}
	}
	// Send to Metarr for tagging (or conversion to desired format)
	mcb := execute.NewMetarrCommandBuilder()
	if config.IsSet(keys.MetarrPreset) {
		mappedCommands, err := mcb.ParseMetarrPreset(dlFiles)
		if err != nil {
			return err
		}
		logging.PrintD(2, "Got mapped commands: %v", mappedCommands)

		if err := mcb.RunMetarr(mappedCommands); err != nil {
			return err
		}
	}
	return nil
}
