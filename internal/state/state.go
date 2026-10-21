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

// crawlState holds each channel's active crawl or download job, keyed by channel ID (names can change mid-job).
var crawlState sync.Map

// crawlLock is an active crawl or download job.
type crawlLock struct {
	cancelActive context.CancelFunc // Cancels in-progress work.
	cancelQueue  context.CancelFunc // Stops queued work from starting.
	stopped      atomic.Bool
	stopCh       chan struct{} // Closed when the job is stopped, which unlike its contexts doesn't happen on unlock.
}

// Context keys for values held by a job's context.
type (
	jobParentKey struct{}
	queueCtxKey  struct{}
	stopChKey    struct{}
)

// TryLockCrawl locks a channel's crawl state if it isn't already locked.
//
// Returns the job's active context, canceled by AbortAll, and an unlock function which must be called when the job ends.
func TryLockCrawl(parent context.Context, channelID int64) (ctx context.Context, unlock func(), ok bool) {
	activeCtx, cancelActive := context.WithCancel(parent)
	activeCtx = context.WithValue(activeCtx, jobParentKey{}, parent)
	queueCtx, cancelQueue := context.WithCancel(activeCtx)
	l := &crawlLock{cancelActive: cancelActive, cancelQueue: cancelQueue, stopCh: make(chan struct{})}
	if _, loaded := crawlState.LoadOrStore(channelID, l); loaded {
		cancelQueue()
		cancelActive()
		return nil, nil, false
	}
	ctx = context.WithValue(activeCtx, queueCtxKey{}, queueCtx)
	return context.WithValue(ctx, stopChKey{}, l.stopCh), func() {
		crawlState.CompareAndDelete(channelID, l)
		cancelQueue()
		cancelActive()
	}, true
}

// ErrStoppedWaiting is returned by LockCrawl when the channel is stopped while waiting to lock it.
var ErrStoppedWaiting = errors.New("stopped while waiting for the channel's running job to finish")

// stopCounts counts each channel's stops, so jobs waiting to lock a channel can tell they were stopped.
var stopCounts sync.Map // Channel ID -> *atomic.Int64.

// stopCount returns the channel's stop counter.
func stopCount(channelID int64) *atomic.Int64 {
	v, _ := stopCounts.LoadOrStore(channelID, new(atomic.Int64))
	n, _ := v.(*atomic.Int64)
	return n
}

// LockCrawl waits for a channel's running job to finish, then locks its crawl state as TryLockCrawl does.
//
// Returns ErrStoppedWaiting if the channel is stopped while waiting, so a stop also cancels waiting jobs.
func LockCrawl(parent context.Context, channelID int64) (ctx context.Context, unlock func(), err error) {
	stops := stopCount(channelID)
	start := stops.Load()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if stops.Load() != start {
			return nil, nil, ErrStoppedWaiting
		}
		if ctx, unlock, ok := TryLockCrawl(parent, channelID); ok {
			return ctx, unlock, nil
		}
		select {
		case <-parent.Done():
			return nil, nil, parent.Err()
		case <-ticker.C:
		}
	}
}

// JobParent returns the context a crawl or download job was started from, which stopping the job does not cancel.
//
// Returns ctx itself if it is not a job context.
func JobParent(ctx context.Context) context.Context {
	if parent, ok := ctx.Value(jobParentKey{}).(context.Context); ok {
		return parent
	}
	return ctx
}

// QueueCtx returns a job's queue context, canceled by StopQueue and AbortAll, for work which must not start once stopped.
//
// Returns ctx itself if it is not a job context.
func QueueCtx(ctx context.Context) context.Context {
	if queueCtx, ok := ctx.Value(queueCtxKey{}).(context.Context); ok {
		return queueCtx
	}
	return ctx
}

// StopRequested returns a channel closed when the job is stopped, which unlike the job's contexts stays open on unlock.
//
// Returns nil if ctx is not a job context.
func StopRequested(ctx context.Context) <-chan struct{} {
	ch, _ := ctx.Value(stopChKey{}).(chan struct{})
	return ch
}

// StopQueue stops a channel's job from starting queued work, and cancels jobs waiting in LockCrawl, letting in-progress
// downloads finish.
//
// Returns false if no job is running.
func StopQueue(channelID int64) bool {
	return stopJob(channelID, false)
}

// AbortAll stops a channel's job, killing in-progress downloads, and cancels jobs waiting in LockCrawl.
//
// Returns false if no job is running.
func AbortAll(channelID int64) bool {
	return stopJob(channelID, true)
}

// stopJob stops a channel's job and any jobs waiting in LockCrawl, also cancelling in-progress work if abort is set.
func stopJob(channelID int64, abort bool) bool {
	v, ok := crawlState.Load(channelID)
	if !ok {
		return false
	}
	l, ok := v.(*crawlLock)
	if !ok {
		logger.Pl.E("Dev Error: Wrong type %T stored in CrawlState for channel ID %d", v, channelID)
		return false
	}
	stopCount(channelID).Add(1)
	if l.stopped.CompareAndSwap(false, true) {
		close(l.stopCh)
	}
	l.cancelQueue()
	if abort {
		l.cancelActive()
	}
	return true
}

// CrawlStateActive reports whether a channel has an active crawl or download job.
func CrawlStateActive(channelID int64) bool {
	_, ok := crawlState.Load(channelID)
	return ok
}

// CrawlActivity returns the IDs of channels with an active crawl or download job, and those being stopped.
func CrawlActivity() (active, stopping []int64) {
	active, stopping = []int64{}, []int64{}
	crawlState.Range(func(k, v any) bool {
		id, _ := k.(int64)
		if l, ok := v.(*crawlLock); ok && l.stopped.Load() {
			stopping = append(stopping, id)
		} else {
			active = append(active, id)
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

// statusUpdates holds status updates for downloads.
var statusUpdates sync.Map

// SetStatusUpdate updates the download state in the map.
func SetStatusUpdate(videoID int64, update models.StatusUpdate) {
	statusUpdates.Store(videoID, update)
}

// TakeStatusUpdate removes and returns a video's status update, if it has one.
func TakeStatusUpdate(videoID int64) (models.StatusUpdate, bool) {
	v, ok := statusUpdates.LoadAndDelete(videoID)
	if !ok {
		return models.StatusUpdate{}, false
	}
	update, ok := v.(models.StatusUpdate)
	return update, ok
}

// DeleteStatusUpdate removes a video's status update.
func DeleteStatusUpdate(videoID int64) {
	statusUpdates.Delete(videoID)
}

// ChannelStatusUpdates returns the status updates held for a channel's videos.
func ChannelStatusUpdates(channelID int64) []models.StatusUpdate {
	var updates []models.StatusUpdate
	statusUpdates.Range(func(_, value any) bool {
		if update, ok := value.(models.StatusUpdate); ok && update.ChannelID == channelID {
			updates = append(updates, update)
		}
		return true
	})
	return updates
}

// GetStatusUpdate returns the current download status.
func GetStatusUpdate(videoID int64) (stateFromMap models.StatusUpdate) {
	s, ok := statusUpdates.Load(videoID)
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

// ClaimActiveVideoDownload marks a video as downloading, returning false if it already is.
func ClaimActiveVideoDownload(videoID int64, status models.DLStatus) bool {
	_, loaded := activeVideoDownloadStatus.LoadOrStore(videoID, status)
	return !loaded
}

// DeleteActiveVideoDownloadStatus deletes the entry in the map for this video.
func DeleteActiveVideoDownloadStatus(videoID int64) {
	activeVideoDownloadStatus.Delete(videoID)
}

// ActiveVideoDownloadStatusExists checks if the current video is already being downloaded.
func ActiveVideoDownloadStatusExists(videoID int64) bool {
	_, ok := activeVideoDownloadStatus.Load(videoID)
	return ok
}
