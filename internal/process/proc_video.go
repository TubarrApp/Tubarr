package process

import (
	"context"
	"time"

	"tubarr/internal/downloads"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// processVideo processes video downloads.
func processVideo(ctx context.Context, v *models.Video, dlTracker *downloads.DownloadTracker) error {
	if v == nil {
		logging.I("Null video entered")
		return nil
	}

	logging.I("Processing video download for URL: %s", v.URL)

	dl, err := downloads.NewVideoDownload(ctx, v, dlTracker, &downloads.Options{
		MaxRetries:    3,
		RetryInterval: 5 * time.Second,
	})
	if err != nil {
		return err
	}

	if err := dl.Execute(); err != nil {
		return err
	}

	logging.D(1, "Successfully processed and marked as downloaded: %s", v.URL)
	return nil
}
