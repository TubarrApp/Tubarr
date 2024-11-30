package downloads

import (
	"context"
	"database/sql"
	"time"
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
	dlstore    models.DownloadStore
	downloader string
}

// NewDownloadTracker returns the model used for tracking downloads.
func NewDownloadTracker(store models.DownloadStore, externalDler string) *DownloadTracker {
	return &DownloadTracker{
		db:         store.GetDB(),
		updates:    make(chan models.StatusUpdate, 100),
		batchSize:  50,
		flushTimer: 500 * time.Millisecond,
		done:       make(chan struct{}),
		dlstore:    store,
		downloader: externalDler,
	}
}

// Start starts download tracking.
func (t *DownloadTracker) Start() {
	go t.processUpdates()
}

// Stop stops download tracking.
func (t *DownloadTracker) Stop() {
	close(t.done)
}

// UpdateStatus updates the status of a single video.
func (t *DownloadTracker) UpdateStatus(d *Download) {
	switch d.Type {
	case TypeJSON:
		return
	case TypeVideo:
		t.updates <- models.StatusUpdate{
			VideoID:  d.Video.ID,
			VideoURL: d.Video.URL,
			Status:   d.Video.DownloadStatus.Status,
			Percent:  d.Video.DownloadStatus.Pct,
		}
	}
}

// processUpdates processes download status updates.
func (t *DownloadTracker) processUpdates() {
	ticker := time.NewTicker(t.flushTimer)
	defer ticker.Stop()
	var lastUpdate models.StatusUpdate

	for {
		select {
		case <-t.done:
			if lastUpdate.VideoID != 0 {
				t.flushUpdates([]models.StatusUpdate{lastUpdate})
			}
			return
		case update := <-t.updates:
			lastUpdate = update
		case <-ticker.C:
			if lastUpdate.VideoID != 0 {
				logging.I("Status update for video with URL %q: Status: %q Percentage: %.1f",
					lastUpdate.VideoURL, lastUpdate.Status, lastUpdate.Percent)
				t.flushUpdates([]models.StatusUpdate{lastUpdate})
			}
		}
	}
}

// flushUpdates flushes pending download status updates to the database.
func (t *DownloadTracker) flushUpdates(updates []models.StatusUpdate) {
	if len(updates) == 0 {
		return
	}

	// Add context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Retry logic for transient failures
	backoff := time.Millisecond * 100
	maxRetries := 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		if err := t.dlstore.UpdateDownloadStatuses(ctx, updates); err != nil {
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
