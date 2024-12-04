package downloads

import (
	"context"
	"database/sql"
	"time"

	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// DownloadTracker is the model holding data related to download tracking.
type DownloadTracker struct {
	db         *sql.DB
	updates    chan models.StatusUpdate
	batchSize  int
	flushTimer time.Duration
	done       chan struct{}
	dlStore    interfaces.DownloadStore
	downloader string
}

// NewDownloadTracker returns the model used for tracking downloads.
func NewDownloadTracker(store interfaces.DownloadStore, externalDler string) *DownloadTracker {
	return &DownloadTracker{
		db:         store.GetDB(),
		updates:    make(chan models.StatusUpdate, 100),
		batchSize:  50,
		flushTimer: 500 * time.Millisecond,
		done:       make(chan struct{}),
		dlStore:    store,
		downloader: externalDler,
	}
}

// Start starts download tracking.
func (t *DownloadTracker) Start(ctx context.Context) {
	go t.processUpdates(ctx)
}

// Stop stops download tracking.
func (t *DownloadTracker) Stop() {
	close(t.done)
}

// sendUpdate constructs the update and sends it into the processing channel.
func (t *DownloadTracker) sendUpdate(v *models.Video) {
	t.updates <- models.StatusUpdate{
		VideoID:  v.ID,
		VideoURL: v.URL,
		Status:   v.DownloadStatus.Status,
		Percent:  v.DownloadStatus.Pct,
		Error:    v.DownloadStatus.Error,
	}
}

// processUpdates processes download status updates.
func (t *DownloadTracker) processUpdates(ctx context.Context) {
	ticker := time.NewTicker(t.flushTimer)
	defer ticker.Stop()
	var lastUpdate models.StatusUpdate
	var newUpdate models.StatusUpdate

	for {
		select {
		case <-t.done:
			if lastUpdate.VideoID != 0 {
				t.flushUpdates(ctx, []models.StatusUpdate{lastUpdate})
			}
			return
		case update := <-t.updates:
			newUpdate = update
		case <-ticker.C:
			if newUpdate != lastUpdate {
				lastUpdate = newUpdate

				logging.I("Status update for video with URL %q:\nStatus: %s\nPercentage: %.1f\nError: %v",
					lastUpdate.VideoURL, lastUpdate.Status, lastUpdate.Percent, lastUpdate.Error)

				t.flushUpdates(ctx, []models.StatusUpdate{lastUpdate})
			}
		}
	}
}

// flushUpdates flushes pending download status updates to the database.
func (t *DownloadTracker) flushUpdates(ctx context.Context, updates []models.StatusUpdate) {
	if len(updates) == 0 {
		return
	}

	// Add context with timeout
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Retry logic for transient failures
	backoff := time.Millisecond * 100
	maxRetries := 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := t.dlStore.UpdateDownloadStatuses(ctx, updates); err != nil {
			if attempt == maxRetries-1 {
				logging.E(0, "Failed to update download statuses after %d attempts: %v", maxRetries, err)
				return
			}
			logging.W("Retrying update after failure (attempt %d/%d): %v",
				attempt+1, maxRetries, err)
			time.Sleep(backoff * time.Duration(attempt+1))
			continue
		}
		break
	}

	logging.D(2, "Successfully flushed %d status updates", len(updates))
}
