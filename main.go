package main

import (
	"Tubarr/internal/command"
	"Tubarr/internal/config"
	keys "Tubarr/internal/domain/keys"
	browser "Tubarr/internal/utils/browser"
	logging "Tubarr/internal/utils/logging"
	metarr "Tubarr/internal/utils/metarr"
	"fmt"
	"os"

	"github.com/spf13/viper"
)

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
	process()
}

func process() {

	if config.IsSet(keys.ChannelCheckNew) {
		urls := browser.GetNewChannelReleases()
		if err := command.DownloadVideos(urls); err != nil {
			logging.PrintE(0, "Error downloading new videos: %v", err)
		}
	}
	if config.IsSet(keys.MetarrPreset) {
		args, err := metarr.ParseMetarrPreset()
		if err != nil {
			logging.PrintE(0, err.Error())
			return
		}
		if err := command.RunMetarr(args); err != nil {
			logging.PrintE(0, err.Error())
			return
		}
	}
}
