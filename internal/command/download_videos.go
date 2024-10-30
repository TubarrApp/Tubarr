package command

import (
	"Tubarr/internal/config"
	consts "Tubarr/internal/domain/constants"
	keys "Tubarr/internal/domain/keys"
	logging "Tubarr/internal/utils/logging"
	"fmt"
	"os"
)

// DownloadVideos takes in a list of URLs
func DownloadVideos(urls []string) error {

	cookieSource := config.GetString(keys.CookieSource)

	if len(urls) == 0 {
		return nil
	}

	for _, entry := range urls {
		command := consts.GrabLatestCommand(config.GetString(keys.VideoDir), entry, cookieSource)

		logging.PrintI(command.String())

		command.Stdout = os.Stdout
		command.Stderr = os.Stderr

		if err := command.Run(); err != nil {
			return fmt.Errorf("error running command for URL '%s': %v", entry, err)
		}
	}

	return nil
}
