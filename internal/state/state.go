// Package state maintains global sync maps for things like downloads and function locks.
package state

import (
	"sync"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// --- Crawl State --------------------------------------------------------------------------

// Lock out of crawl function
var crawlState sync.Map

const lockKeyCrawl = "crawl"

// LockCrawlState locks the crawl function from use.
func LockCrawlState(channelName string) {
	key := channelName + lockKeyCrawl
	crawlState.Store(key, true)
}

// UnlockCrawlState unlocks the crawl function.
func UnlockCrawlState(channelName string) {
	key := channelName + lockKeyCrawl
	crawlState.Store(key, false)
}

// CrawlStateActive reports whether the crawl function is currently active.
func CrawlStateActive(channelName string) bool {
	key := channelName + lockKeyCrawl

	s, ok := crawlState.Load(key)
	if !ok {
		crawlState.Store(key, false)
		return false
	}

	state, ok := s.(bool)
	if !ok {
		logger.Pl.E("Dev Error: Wrong type %T stored in CrawlState for key %q", s, key)
		return false
	}

	return state
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
