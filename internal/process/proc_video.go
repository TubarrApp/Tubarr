package process

import (
	"context"
	"time"

	"tubarr/internal/downloads"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// processVideo processes video downloads.
func processVideo(procCtx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, dlTracker *downloads.DownloadTracker) (botPauseChannel bool, err error) {
	if v == nil {
		logging.I("Null video entered")
		return false, nil
	}

	logging.I("Processing video download for URL: %s", v.URL)

	dl, err := downloads.NewVideoDownload(procCtx, v, cu, c, dlTracker, &downloads.Options{
		MaxRetries:    3,
		RetryInterval: 5 * time.Second,
	})
	if err != nil {
		return false, err
	}

	if botPauseChannel, err := dl.Execute(); err != nil {
		if botPauseChannel {
			return true, err // Only 'TRUE' bot pause channel path
		}
		return false, err
	}

	logging.D(1, "Successfully processed and marked as downloaded: %s", v.URL)
	return false, nil
}
