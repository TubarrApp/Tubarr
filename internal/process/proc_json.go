package process

import (
	"fmt"
	"time"
	"tubarr/internal/downloads"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// processJSON downloads and processes JSON for a video.
func processJSON(v *models.Video, vs models.VideoStore) error {
	if v == nil {
		logging.I("Null video entered")
		return nil
	}

	logging.D(2, "Processing JSON download for URL: %s", v.URL)

	dl, err := downloads.NewDownload(downloads.TypeJSON, v, &downloads.Options{
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
