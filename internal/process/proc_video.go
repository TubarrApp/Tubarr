package process

import (
	"context"
	"fmt"
	"time"

	"tubarr/internal/downloads"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// processVideo processes video downloads.
func processVideo(ctx context.Context, v *models.Video, vs interfaces.VideoStore, dlTracker *downloads.DownloadTracker) error {
	if v == nil {
		logging.I("Null video entered")
		return nil
	}

	logging.D(2, "Processing video download for URL: %s", v.URL)

	dl, err := downloads.NewDownload(downloads.TypeVideo, ctx, v, dlTracker, &downloads.Options{
		MaxRetries:    3,
		RetryInterval: 5 * time.Second,
	})
	if err != nil {
		return err
	}

	if err := dl.Execute(); err != nil {
		return err
	}

	if err := vs.UpdateVideo(v); err != nil {
		return fmt.Errorf("failed to update video DB entry: %w", err)
	}

	logging.D(1, "Successfully processed and marked as downloaded: %s", v.URL)
	return nil
}
