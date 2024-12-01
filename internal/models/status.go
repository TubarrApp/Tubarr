package models

import "tubarr/internal/domain/consts"

// DLStatus holds the data related to download progress etc.
type DLStatus struct {
	Status consts.DownloadStatus `json:"status"`
	Pct    float64               `json:"percentage"`
	Error  error                 `json:"error"`
}

var DLStatusDefault = DLStatus{
	Status: consts.DLStatusPending,
	Pct:    0.0,
	Error:  nil,
}

// StatusUpdate models updates to the download status of a video.
type StatusUpdate struct {
	VideoID  int64
	VideoURL string
	Status   consts.DownloadStatus
	Percent  float64
	Error    error
}
