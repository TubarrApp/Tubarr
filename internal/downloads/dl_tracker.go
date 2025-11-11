package downloads

import (
	"context"
	"database/sql"
	"errors"
	"tubarr/internal/contracts"
	"tubarr/internal/models"
	"tubarr/internal/state"
	"tubarr/internal/utils/logging"
)

// DownloadTracker is the model holding data related to download tracking.
type DownloadTracker struct {
	db         *sql.DB
	updates    chan models.StatusUpdate
	batchSize  int
	done       chan struct{}
	dlStore    contracts.DownloadStore
	downloader string
}

// NewDownloadTracker returns the model used for tracking downloads.
func NewDownloadTracker(store contracts.DownloadStore, externalDler string) *DownloadTracker {
	return &DownloadTracker{
		db:         store.GetDB(),
		updates:    make(chan models.StatusUpdate, 100),
		batchSize:  50,
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
	if v == nil || v.URL == "" {
		logging.E("Invalid video struct before status update: %+v", v)
		return
	}

	select {
	case <-t.done:
		logging.W("Attempted to send update after tracker stopped (Video %q)", v.URL)
		return
	default:
		t.updates <- models.StatusUpdate{
			VideoID:      v.ID,
			VideoTitle:   v.Title,
			ChannelID:    v.ChannelID,
			ChannelURLID: v.ChannelURLID,
			VideoURL:     v.URL,
			Status:       v.DownloadStatus.Status,
			Percent:      v.DownloadStatus.Percent,
			Error:        v.DownloadStatus.Error,
		}
	}
}

// processUpdates processes download status updates.
func (t *DownloadTracker) processUpdates(ctx context.Context) {
	for {
		select {
		case <-t.done:
			state.StatusUpdate.Range(func(_, v any) bool {
				update, ok := v.(models.StatusUpdate)
				if ok {
					if err := t.dlStore.UpdateDownloadStatus(ctx, update); err != nil {
						logging.E("Failed to store final update for video %q: %v", update.VideoURL, err)
					}
				}
				return true
			})
			return

		case update := <-t.updates:
			logging.I("Status update for video with URL %q:\n\nStatus: %s\nPercentage: %.1f\nError: %v\n",
				update.VideoURL, update.Status, update.Percent, update.Error)
			t.flushUpdates(ctx, update)

		}
	}
}

// flushUpdates flushes pending download status updates to the database.
func (t *DownloadTracker) flushUpdates(ctx context.Context, update models.StatusUpdate) {
	lastUpdate := state.GetStatusUpdate(update.VideoID)

	// Normalize percentage
	update.Percent = min(update.Percent, 100.0)

	// Check if state is different
	if update.Status == lastUpdate.Status &&
		update.Percent == lastUpdate.Percent &&
		errors.Is(update.Error, lastUpdate.Error) {
		return
	}

	// If percentage has increased, assume error cleared.
	if update.Percent > lastUpdate.Percent && lastUpdate.Error != nil {
		update.Error = nil
	}

	// Write to map and database.
	state.SetStatusUpdate(update.VideoID, update)

	if update.Percent == 100.0 || lastUpdate.Status != update.Status {
		if err := t.dlStore.UpdateDownloadStatus(ctx, update); err != nil {
			logging.E("Failed to store update for video %q in database", update.VideoURL)
		}
	}
}
