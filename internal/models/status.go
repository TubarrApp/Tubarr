package models

import "tubarr/internal/domain/consts"

// DLStatus holds the data related to download progress etc.
type DLStatus struct {
	Status   consts.DownloadStatus `json:"status"`
	Percent  float64               `json:"percentage"`
	Stage    string                `json:"stage,omitempty"` // What a queued video is waiting on, not stored.
	ETA      int64                 `json:"eta"`             // Seconds left, -1 when unknown, not stored.
	Speed    float64               `json:"speed"`           // Bytes per second while downloading, 0 when unknown, not stored.
	Error    error                 `json:"-"`
	ErrorMsg string                `json:"error,omitempty"` // Why the last attempt failed, as shown in the web UI.
}

// StatusUpdate models updates to the download status of a video.
type StatusUpdate struct {
	VideoID  int64
	VideoURL string
	Status   consts.DownloadStatus
	Percent  float64
	Error    error
}
