package main

import (
	"Tubarr/internal/command"
	"Tubarr/internal/config"
	keys "Tubarr/internal/domain/keys"
	"Tubarr/internal/models"
	browser "Tubarr/internal/utils/browser"
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"os"

	"github.com/spf13/viper"
)

// main is the program entrypoint
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
	if err := process(); err != nil {
		logging.PrintE(0, err.Error())
		os.Exit(1)
	}
}

// process begins the main Tubarr program
func process() error {

	var dlFiles []models.DownloadedFiles
	var err error

	if config.IsSet(keys.ChannelCheckNew) {
		urls := browser.GetNewReleases()
		dlFiles, err = command.DownloadVideos(urls)
		if err != nil {
			return fmt.Errorf("error downloading new videos: %w", err)
		}
	}
	mcb := command.NewMetarrCommandBuilder()
	if config.IsSet(keys.MetarrPreset) {
		mappedCommands, err := mcb.ParseMetarrPreset(dlFiles)
		if err != nil {
			return err
		} else if err := mcb.RunMetarr(mappedCommands); err != nil {
			return err
		}
	}
	return nil
}
