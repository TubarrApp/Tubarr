// Package state maintains global sync maps for things like downloads and function locks.
package state

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// --- Crawl State --------------------------------------------------------------------------

// crawlState holds each channel's active crawl or download job, keyed by channel name.
var crawlState sync.Map

// crawlLock is an active crawl or download job.
type crawlLock struct {
	cancel  context.CancelFunc
	stopped atomic.Bool
}

// jobParentKey is the context key holding the context a crawl or download job was started from.
type jobParentKey struct{}

// TryLockCrawl locks a channel's crawl state if it isn't already locked.
//
// Returns a context cancelled by StopCrawl, and an unlock function which must be called when the job ends.
func TryLockCrawl(parent context.Context, channelName string) (ctx context.Context, unlock func(), ok bool) {
	ctx, cancel := context.WithCancel(parent)
	l := &crawlLock{cancel: cancel}
	if _, loaded := crawlState.LoadOrStore(channelName, l); loaded {
		cancel()
		return nil, nil, false
	}
	return context.WithValue(ctx, jobParentKey{}, parent), func() {
		crawlState.CompareAndDelete(channelName, l)
		cancel()
	}, true
}

// ErrStoppedWaiting is returned by LockCrawl when the channel is stopped while waiting to lock it.
var ErrStoppedWaiting = errors.New("stopped while waiting for the channel's running job to finish")

// stopCounts counts each channel's stops, so jobs waiting to lock a channel can tell they were stopped.
var stopCounts sync.Map // Channel name -> *atomic.Int64.

// stopCount returns the channel's stop counter.
func stopCount(channelName string) *atomic.Int64 {
	v, _ := stopCounts.LoadOrStore(channelName, new(atomic.Int64))
	n, _ := v.(*atomic.Int64)
	return n
}

// LockCrawl waits for a channel's running job to finish, then locks its crawl state as TryLockCrawl does.
//
// Returns ErrStoppedWaiting if the channel is stopped while waiting, so a stop also cancels waiting jobs.
func LockCrawl(parent context.Context, channelName string) (ctx context.Context, unlock func(), err error) {
	stops := stopCount(channelName)
	start := stops.Load()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if stops.Load() != start {
			return nil, nil, ErrStoppedWaiting
		}
		if ctx, unlock, ok := TryLockCrawl(parent, channelName); ok {
			return ctx, unlock, nil
		}
		select {
		case <-parent.Done():
			return nil, nil, parent.Err()
		case <-ticker.C:
		}
	}
}

// JobParent returns the context a crawl or download job was started from, which StopCrawl does not cancel.
//
// Returns ctx itself if it is not a job context.
func JobParent(ctx context.Context) context.Context {
	if parent, ok := ctx.Value(jobParentKey{}).(context.Context); ok {
		return parent
	}
	return ctx
}

// StopCrawl stops a channel's active crawl or download job, and any jobs waiting in LockCrawl, returning false if none is running.
func StopCrawl(channelName string) bool {
	v, ok := crawlState.Load(channelName)
	if !ok {
		return false
	}
	l, ok := v.(*crawlLock)
	if !ok {
		logger.Pl.E("Dev Error: Wrong type %T stored in CrawlState for channel %q", v, channelName)
		return false
	}
	stopCount(channelName).Add(1)
	l.stopped.Store(true)
	l.cancel()
	return true
}

// CrawlStateActive reports whether a channel has an active crawl or download job.
func CrawlStateActive(channelName string) bool {
	_, ok := crawlState.Load(channelName)
	return ok
}

// CrawlActivity returns the names of channels with an active crawl or download job, and those being stopped.
func CrawlActivity() (active, stopping []string) {
	active, stopping = []string{}, []string{}
	crawlState.Range(func(k, v any) bool {
		name, _ := k.(string)
		if l, ok := v.(*crawlLock); ok && l.stopped.Load() {
			stopping = append(stopping, name)
		} else {
			active = append(active, name)
		}
		return true
	})
	return active, stopping
}

// ---- Jitter Times --------------------------------------------------------------------------

// Cached jitter times per channel
var watchdogJitter sync.Map

type jitterCache struct {
	jitter   int
	urlCount int
}

// GetOrComputeJitter returns existing jitter or recalculates if URL count changed.
func GetOrComputeJitter(channelID int64, chanURLCount int, compute func() int) int {
	if val, ok := watchdogJitter.Load(channelID); ok {
		if jc, ok := val.(jitterCache); ok {
			if jc.urlCount == chanURLCount {
				return jc.jitter
			}
		}
	}
	// Recompute on cache miss or URL count changed
	j := compute()
	watchdogJitter.Store(channelID, jitterCache{jitter: j, urlCount: chanURLCount})
	return j
}

// --- Status Update --------------------------------------------------------------------------

// StatusUpdateCache holds status updates for downloads.
var StatusUpdateCache sync.Map

// SetStatusUpdate updates the download state in the map.
func SetStatusUpdate(videoID int64, update models.StatusUpdate) {
	StatusUpdateCache.Store(videoID, update)
}

// GetStatusUpdate returns the current download status.
func GetStatusUpdate(videoID int64) (stateFromMap models.StatusUpdate) {
	s, ok := StatusUpdateCache.Load(videoID)
	if !ok {
		return models.StatusUpdate{}
	}

	currentState, ok := s.(models.StatusUpdate)
	if !ok {
		logger.Pl.E("Dev Error: Wrong type %T stored in StatusUpdate for video ID %d", s, videoID)
		return models.StatusUpdate{}
	}

	return currentState
}

// --- Download Status --------------------------------------------------------------------------

// State of current video download
var activeVideoDownloadStatus sync.Map

// SetActiveVideoDownloadStatus updates the download state in the map.
func SetActiveVideoDownloadStatus(videoID int64, status models.DLStatus) {
	activeVideoDownloadStatus.Store(videoID, status)
}

// DeleteActiveVideoDownloadStatus deletes the entry in the map for this video.
func DeleteActiveVideoDownloadStatus(videoID int64) {
	activeVideoDownloadStatus.Delete(videoID)
}

// // ResetActiveVideoDownloadStatus resets download status model for the video.
// func ResetActiveVideoDownloadStatus(v *models.Video) {
// 	activeVideoDownloadStatus.Store(v.ID, models.DLStatus{
// 		Status:  v.DownloadStatus.Status,
// 		Percent: v.DownloadStatus.Percent,
// 		Error:   v.DownloadStatus.Error,
// 	})
// }

// ActiveVideoDownloadStatusExists checks if the current video is already being downloaded.
func ActiveVideoDownloadStatusExists(videoID int64) bool {
	_, ok := activeVideoDownloadStatus.Load(videoID)
	return ok
}

// // GetActiveVideoDownloadStatus returns the current download status.
// func GetActiveVideoDownloadStatus(v *models.Video) (stateFromMap models.DLStatus) {
// 	s, ok := activeVideoDownloadStatus.Load(v.ID)
// 	if !ok {
// 		ResetVideoDownloadStatus(v)
// 		s, ok = activeVideoDownloadStatus.Load(v.ID)
// 		if !ok {
// 			logger.Pl.E("Could not get download status from map")
// 			return models.DLStatus{
// 				Status:  consts.DLStatusFailed,
// 				Percent: 0.0,
// 				Error:   fmt.Errorf("could not get download status for video %q from map", v.URL),
// 			}
// 		}
// 	}

// 	currentState, ok := s.(models.DLStatus)
// 	if !ok {
// 		logger.Pl.E("Dev Error: Wrong type %T stored in StatusUpdate for video %q", s, v.URL)
// 		return models.DLStatus{
// 			Status:  consts.DLStatusFailed,
// 			Percent: 0.0,
// 			Error:   fmt.Errorf("could not get download status for video %q from map", v.URL),
// 		}
// 	}

// 	return currentState
// }
