package models

import "tubarr/internal/domain/consts"

// DLStatus holds the data related to download progress etc.
type DLStatus struct {
	Status consts.DownloadStatus `json:"status"`
	Pct    float64               `json:"percentage"`
}

var DLStatusDefault = DLStatus{
	Status: consts.DLStatusPending,
	Pct:    0.0,
}

type VideoMap struct {
	IDs  []int64
	Data map[int64]*Video
}

// NewVideoMap creates a VideoMap from a slice of videos.
func NewVideoMap(videos []*Video) VideoMap {
	ids := make([]int64, len(videos))
	data := make(map[int64]*Video, len(videos))

	for i, v := range videos {
		ids[i] = v.ID
		data[v.ID] = v
	}

	return VideoMap{
		IDs:  ids,
		Data: data,
	}
}

type StatusUpdate struct {
	VideoID  int64
	VideoURL string
	Status   consts.DownloadStatus
	Percent  float64
	Error    error
}
