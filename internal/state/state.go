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
func TryLockCrawl(parent context.Context, channelName string) (ctx context.Context, unlock func(), ok bool) {
	activeCtx, cancelActive := context.WithCancel(parent)
	activeCtx = context.WithValue(activeCtx, jobParentKey{}, parent)
	queueCtx, cancelQueue := context.WithCancel(activeCtx)
	l := &crawlLock{cancelActive: cancelActive, cancelQueue: cancelQueue, stopCh: make(chan struct{})}
	if _, loaded := crawlState.LoadOrStore(channelName, l); loaded {
		cancelQueue()
		cancelActive()
		return nil, nil, false
	}
	ctx = context.WithValue(activeCtx, queueCtxKey{}, queueCtx)
	return context.WithValue(ctx, stopChKey{}, l.stopCh), func() {
		crawlState.CompareAndDelete(channelName, l)
		cancelQueue()
		cancelActive()
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
func StopQueue(channelName string) bool {
	return stopJob(channelName, false)
}

// AbortAll stops a channel's job, killing in-progress downloads, and cancels jobs waiting in LockCrawl.
//
// Returns false if no job is running.
func AbortAll(channelName string) bool {
	return stopJob(channelName, true)
}

// stopJob stops a channel's job and any jobs waiting in LockCrawl, also cancelling in-progress work if abort is set.
func stopJob(channelName string, abort bool) bool {
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
