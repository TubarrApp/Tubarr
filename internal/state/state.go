// Package state holds which channels are crawling, and each channel's crawl jitter.
package state

import (
	"context"
	"sync"
	"sync/atomic"
	"tubarr/internal/domain/logger"
)

// --- Crawl Locks --------------------------------------------------------------------------

// crawlLocks holds each channel's running crawl, so a channel only crawls once at a time, keyed by channel ID (names can
// change mid-crawl).
var crawlLocks sync.Map

// crawlLock is a channel's running crawl.
type crawlLock struct {
	stop    context.CancelFunc // Stops the crawl.
	stopped atomic.Bool
}

// TryLockCrawl locks a channel for a crawl if it isn't already crawling.
//
// Returns the crawl's context, canceled by StopCrawl, and an unlock function which must be called when the crawl ends.
func TryLockCrawl(parent context.Context, channelID int64) (ctx context.Context, unlock func(), ok bool) {
	ctx, stop := context.WithCancel(parent)
	l := &crawlLock{stop: stop}
	if _, loaded := crawlLocks.LoadOrStore(channelID, l); loaded {
		stop()
		return nil, nil, false
	}
	return ctx, func() {
		crawlLocks.CompareAndDelete(channelID, l)
		stop()
	}, true
}

// StopCrawl stops a channel's running crawl.
//
// Returns false if the channel isn't crawling.
func StopCrawl(channelID int64) bool {
	v, ok := crawlLocks.Load(channelID)
	if !ok {
		return false
	}
	l, ok := v.(*crawlLock)
	if !ok {
		logger.Pl.DevE("Wrong type %T stored in crawlLocks for channel ID %d", v, channelID)
		return false
	}
	l.stopped.Store(true)
	l.stop()
	return true
}

// CrawlingChannels returns the IDs of channels with a running crawl, and of those being stopped.
func CrawlingChannels() (active, stopping []int64) {
	active = []int64{}
	stopping = []int64{}
	crawlLocks.Range(func(k, v any) bool {
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
