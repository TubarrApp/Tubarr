package process

import (
	"context"
	"fmt"
	"time"

	"tubarr/internal/domain/consts"
	"tubarr/internal/downloads"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
)

// processJSON downloads and processes JSON for a video.
func processJSON(ctx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, vs interfaces.VideoStore, dirParser *parsing.Directory, dlTracker *downloads.DownloadTracker) (proceed bool, err error) {
	if v == nil {
		logging.I("Null video entered")
		return false, nil
	}
	logging.D(2, "Processing JSON download for URL: %s", v.URL)

	dl, err := downloads.NewJSONDownload(ctx, v, cu, c, dlTracker, &downloads.Options{
		MaxRetries:    3,
		RetryInterval: 5 * time.Second,
	})
	if err != nil {
		return false, err
	}

	if err := dl.Execute(); err != nil {
		return false, err
	}

	jsonValid, err := parseAndStoreJSON(v)
	if err != nil {
		logging.E(0, "JSON parsing/storage failed for %q: %v", v.URL, err)
	}

	passedFilters, err := filterRequests(v, cu, c, dirParser)
	if err != nil {
		logging.E(0, "filter operation checks failed for %q: %v", v.URL, err)
	}

	if !jsonValid || !passedFilters { // JSON failed checks, exclude video

		v.DownloadStatus.Status = consts.DLStatusCompleted
		v.DownloadStatus.Pct = 100.0
		v.Finished = true
		v.WasSkipped = true

		if v.ID, err = vs.AddVideo(v, c); err != nil {
			return false, fmt.Errorf("failed to update ignored video in DB: %w", err)
		}
		return false, nil
	}

	v.MoveOpOutputDir, v.MoveOpChannelURL = checkMoveOps(v, dirParser)

	if v.ID, err = vs.AddVideo(v, c); err != nil {
		return false, fmt.Errorf("failed to update video DB entry: %w", err)
	}

	logging.S(0, "Processed metadata for: %s", v.URL)
	return true, nil
}
