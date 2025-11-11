// Package state maintains global sync maps for things like downloads and function locks.
package state

import (
	"sync"
	"tubarr/internal/domain/consts"
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

// ResetStatusUpdate resets download status model for the video.
func ResetStatusUpdate(v *models.Video) {
	StatusUpdate.Store(v.ID, models.StatusUpdate{
		VideoID:      v.ID,
		VideoTitle:   v.Title,
		ChannelID:    v.ChannelID,
		ChannelURLID: v.ChannelURLID,
		VideoURL:     v.URL,
		Status:       consts.DLStatusPending,
		Percent:      0.0,
		Error:        nil,
	})
}

// GetStatusUpdate returns the current download status.
func GetStatusUpdate(inputState models.StatusUpdate) (stateFromMap models.StatusUpdate, success bool) {
	s, ok := StatusUpdate.Load(inputState.VideoID)
	if !ok {
		SetStatusUpdate(inputState.VideoID, inputState)
		if s, ok = StatusUpdate.Load(inputState.VideoID); !ok {
			logging.E("Could not load download status for %q from map", inputState.VideoURL)
			return inputState, false
		}
	}

	currentState, ok := s.(models.StatusUpdate)
	if !ok {
		logging.E("Dev Error: Wrong type %T stored in StatusUpdate for video %q", s, inputState.VideoURL)
		return inputState, false
	}

	return currentState, true
}

// --- Download Status --------------------------------------------------------------------------
// State of current video download
var VideoDownloadStatus sync.Map

// SetVideoDownloadStatus updates the download state in the map.
func SetVideoDownloadStatus(videoID int64, status models.DLStatus) {
	VideoDownloadStatus.Store(videoID, status)
}

// DeleteVideoDownloadStatus deletes the entry in the map for this video.
func DeleteVideoDownloadStatus(videoID int64) {
	VideoDownloadStatus.Delete(videoID)
}

// // ResetVideoDownloadStatus resets download status model for the video.
// func ResetVideoDownloadStatus(v *models.Video) {
// 	VideoDownloadStatus.Store(v.ID, models.DLStatus{
// 		Status:  v.DownloadStatus.Status,
// 		Percent: v.DownloadStatus.Percent,
// 		Error:   v.DownloadStatus.Error,
// 	})
// }

// VideoDownloadStatusExists checks if the current video is already being downloaded.
func VideoDownloadStatusExists(videoID int64) bool {
	_, ok := VideoDownloadStatus.Load(videoID)
	return ok
}

// // GetVideoDownloadStatus returns the current download status.
// func GetVideoDownloadStatus(v *models.Video) (stateFromMap models.DLStatus) {
// 	s, ok := VideoDownloadStatus.Load(v.ID)
// 	if !ok {
// 		ResetVideoDownloadStatus(v)
// 		s, ok = VideoDownloadStatus.Load(v.ID)
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
