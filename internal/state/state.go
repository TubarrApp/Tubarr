// Package state maintains global sync maps for things like downloads and function locks.
package state

import (
	"sync"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// --- Crawl State --------------------------------------------------------------------------
// Lock out of crawl function
var CrawlState sync.Map

const lockKeyCrawl = "crawl"

// LockCrawlState locks the crawl function from use.
func LockCrawlState(channelName string) {
	key := channelName + lockKeyCrawl
	CrawlState.Store(key, true)
}

// UnlockCrawlState unlocks the crawl function.
func UnlockCrawlState(channelName string) {
	key := channelName + lockKeyCrawl
	CrawlState.Store(key, false)
}

// CheckCrawlState reports whether the crawl function is currently active.
func CheckCrawlState(channelName string) bool {
	key := channelName + lockKeyCrawl

	s, ok := CrawlState.Load(key)
	if !ok {
		CrawlState.Store(key, false)
		return false
	}

	state, ok := s.(bool)
	if !ok {
		logging.E("Dev Error: Wrong type %T stored in CrawlState for key %q", s, key)
		return false
	}

	return state
}

// --- Status Update --------------------------------------------------------------------------
// State of current download
var StatusUpdate sync.Map

// SetStatusUpdate updates the download state in the map.
func SetStatusUpdate(videoID int64, update models.StatusUpdate) {
	StatusUpdate.Store(videoID, update)
}

// GetStatusUpdate returns the current download status.
func GetStatusUpdate(videoID int64) (stateFromMap models.StatusUpdate) {
	s, ok := StatusUpdate.Load(videoID)
	if !ok {
		return models.StatusUpdate{}
	}

	currentState, ok := s.(models.StatusUpdate)
	if !ok {
		logging.E("Dev Error: Wrong type %T stored in StatusUpdate for video ID %d", s, videoID)
		return models.StatusUpdate{}
	}

	return currentState
}

// --- Download Status --------------------------------------------------------------------------
// State of current video download
var ActiveVideoDownloadStatus sync.Map

// SetActiveVideoDownloadStatus updates the download state in the map.
func SetActiveVideoDownloadStatus(videoID int64, status models.DLStatus) {
	ActiveVideoDownloadStatus.Store(videoID, status)
}

// DeleteActiveVideoDownloadStatus deletes the entry in the map for this video.
func DeleteActiveVideoDownloadStatus(videoID int64) {
	ActiveVideoDownloadStatus.Delete(videoID)
}

// // ResetActiveVideoDownloadStatus resets download status model for the video.
// func ResetActiveVideoDownloadStatus(v *models.Video) {
// 	ActiveVideoDownloadStatus.Store(v.ID, models.DLStatus{
// 		Status:  v.DownloadStatus.Status,
// 		Percent: v.DownloadStatus.Percent,
// 		Error:   v.DownloadStatus.Error,
// 	})
// }

// ActiveVideoDownloadStatusExists checks if the current video is already being downloaded.
func ActiveVideoDownloadStatusExists(videoID int64) bool {
	_, ok := ActiveVideoDownloadStatus.Load(videoID)
	return ok
}

// // GetActiveVideoDownloadStatus returns the current download status.
// func GetActiveVideoDownloadStatus(v *models.Video) (stateFromMap models.DLStatus) {
// 	s, ok := ActiveVideoDownloadStatus.Load(v.ID)
// 	if !ok {
// 		ResetVideoDownloadStatus(v)
// 		s, ok = ActiveVideoDownloadStatus.Load(v.ID)
// 		if !ok {
// 			logging.E("Could not get download status from map")
// 			return models.DLStatus{
// 				Status:  consts.DLStatusFailed,
// 				Percent: 0.0,
// 				Error:   fmt.Errorf("could not get download status for video %q from map", v.URL),
// 			}
// 		}
// 	}

// 	currentState, ok := s.(models.DLStatus)
// 	if !ok {
// 		logging.E("Dev Error: Wrong type %T stored in StatusUpdate for video %q", s, v.URL)
// 		return models.DLStatus{
// 			Status:  consts.DLStatusFailed,
// 			Percent: 0.0,
// 			Error:   fmt.Errorf("could not get download status for video %q from map", v.URL),
// 		}
// 	}

// 	return currentState
// }
