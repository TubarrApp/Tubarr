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

// processJSON downloads and processes JSON for a video.
func processJSON(ctx context.Context, v *models.Video, vs interfaces.VideoStore, dlTracker *downloads.DownloadTracker) error {
	if v == nil {
		logging.I("Null video entered")
		return nil
	}

	logging.D(2, "Processing JSON download for URL: %s", v.URL)

	dl, err := downloads.NewDownload(downloads.TypeJSON, ctx, v, dlTracker, &downloads.Options{
		MaxRetries:    3,
		RetryInterval: 5 * time.Second,
	})
	if err != nil {
		return err
	}

	if err := dl.Execute(); err != nil {
		return err
	}

	_, err = parseAndStoreJSON(v)
	if err != nil {
		logging.E(0, "JSON parsing/storage failed for %q: %v", v.URL, err)
	}

	if v.ID, err = vs.AddVideo(v); err != nil {
		return fmt.Errorf("failed to update video DB entry: %w", err)
	}

	logging.S(0, "Processed metadata for: %s", v.URL)
	return nil
}
