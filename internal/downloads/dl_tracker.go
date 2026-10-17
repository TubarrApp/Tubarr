package downloads

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/state"
)

// DownloadTracker is the model holding data related to download tracking.
type DownloadTracker struct {
	updates  chan models.StatusUpdate
	done     chan struct{}
	stopOnce sync.Once
	started  atomic.Bool
	finished chan struct{}
	dlStore  contracts.DownloadStore
	videoIDs sync.Map // IDs of videos this tracker has sent updates for.
}

// NewDownloadTracker returns the model used for tracking downloads.
func NewDownloadTracker(store contracts.DownloadStore) *DownloadTracker {
	return &DownloadTracker{
		updates:  make(chan models.StatusUpdate, 100),
		done:     make(chan struct{}),
		finished: make(chan struct{}),
		dlStore:  store,
	}
}

// GetStore returns the DownloadStore for accessing database operations.
func (t *DownloadTracker) GetStore() contracts.DownloadStore {
	return t.dlStore
}

// Start starts download tracking.
func (t *DownloadTracker) Start(ctx context.Context) {
	if t.started.CompareAndSwap(false, true) {
		go t.processUpdates(ctx)
	}
}

// Stop stops download tracking, once the updates already sent are stored.
func (t *DownloadTracker) Stop() {
	t.stopOnce.Do(func() { close(t.done) })
	if t.started.Load() {
		<-t.finished
	}
}

// SetStatus sets the video's download status fields and sends the update.
func (t *DownloadTracker) SetStatus(v *models.Video, status consts.DownloadStatus, pct float64, stage string, err error) {
	v.DownloadStatus = models.DLStatus{Status: status, Percent: pct, Stage: stage, Error: err}
	t.SendUpdate(v)
}

// SendUpdate constructs the update and sends it into the processing channel.
func (t *DownloadTracker) SendUpdate(v *models.Video) {
	if v == nil || v.URL == "" {
		logger.Pl.E("Invalid video struct before status update: %+v", v)
		return
	}

	update := models.StatusUpdate{
		VideoID:      v.ID,
		VideoTitle:   v.Title,
		ChannelID:    v.ChannelID,
		ChannelURLID: v.ChannelURLID,
		ChannelURL:   v.ChannelURL,
		VideoURL:     v.URL,
		Status:       v.DownloadStatus.Status,
		Percent:      v.DownloadStatus.Percent,
		Stage:        v.DownloadStatus.Stage,
		Error:        v.DownloadStatus.Error,
	}
	select {
	case <-t.done:
		logger.Pl.W("Attempted to send update after tracker stopped (Video %q)", v.URL)
	case t.updates <- update:
	}
}

// processUpdates processes download status updates.
//
//nolint:contextcheck
func (t *DownloadTracker) processUpdates(ctx context.Context) {
	defer close(t.finished)
	for {
		select {
		case <-t.done:
			t.drain()
			return

		case update := <-t.updates:
			t.flushUpdates(ctx, update)
		}
	}
}

// drain stores updates still queued, then the latest status of each of this tracker's videos still cached.
//
// Uses a fresh context, as the original may already be canceled when Stop is called.
func (t *DownloadTracker) drain() {
	drainCtx := context.Background()
	for {
		select {
		case update := <-t.updates:
			t.flushUpdates(drainCtx, update)
			continue
		default:
		}
		break
	}

	t.videoIDs.Range(func(id, _ any) bool {
		videoID, _ := id.(int64)
		v, ok := state.StatusUpdateCache.LoadAndDelete(videoID)
		if !ok {
			return true
		}
		if update, ok := v.(models.StatusUpdate); ok {
			if err := t.dlStore.UpdateDownloadStatus(drainCtx, update); err != nil {
				logger.Pl.E("Failed to store final update for video %q: %v", update.VideoURL, err)
			}
		}
		return true
	})
}

// flushUpdates flushes pending download status updates to the database.
func (t *DownloadTracker) flushUpdates(ctx context.Context, update models.StatusUpdate) {
	lastUpdate := state.GetStatusUpdate(update.VideoID)

	// Normalize percentage.
	update.Percent = min(update.Percent, 100.0)

	// Check if state is different.
	if update.Status == lastUpdate.Status &&
		update.Percent == lastUpdate.Percent &&
		update.Stage == lastUpdate.Stage &&
		errors.Is(update.Error, lastUpdate.Error) {
		return
	}

	// Status changes are logged at info level, progress only when debugging.
	const statusFmt = "Status update for video with URL %q:\n\nStatus: %s\nPercentage: %.1f\nError: %v\n"
	if update.Status != lastUpdate.Status {
		logger.Pl.I(statusFmt, update.VideoURL, update.Status, update.Percent, update.Error)
	} else {
		logger.Pl.D(2, statusFmt, update.VideoURL, update.Status, update.Percent, update.Error)
	}

	// If percentage has increased, assume error cleared.
	if update.Percent > lastUpdate.Percent && lastUpdate.Error != nil {
		update.Error = nil
	}

	// A new attempt starting means any earlier error has cleared.
	if update.Status == consts.DLStatusQueued || (update.Status == consts.DLStatusDownloading && lastUpdate.Status != consts.DLStatusDownloading) {
		update.Error = nil
	}

	// Write to map and database.
	t.videoIDs.Store(update.VideoID, struct{}{})
	state.SetStatusUpdate(update.VideoID, update)

	if update.Percent == 100.0 || lastUpdate.Status != update.Status {
		if err := t.dlStore.UpdateDownloadStatus(ctx, update); err != nil {
			logger.Pl.E("Failed to store update for video %q in database: %v", update.VideoURL, err)
			return
		}
		// Finished statuses are stored, so they no longer need caching (the cache feeds the active downloads view).
		if update.Status != consts.DLStatusQueued && update.Status != consts.DLStatusDownloading {
			state.StatusUpdateCache.Delete(update.VideoID)
		}
	}
}
