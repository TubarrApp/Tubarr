package models

import (
	"os"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
)

// Video contains fields relating to a video, and a pointer to the channel it belongs to.
//
// Matches the order of the DB table, do not alter.
type Video struct {
	ID                  int64                 `json:"id" db:"id"`
	ChannelID           int64                 `json:"channel_id" db:"channel_id"`
	ChannelURLID        int64                 `json:"channel_url_id" db:"channel_url_id"`
	ThumbnailURL        string                `json:"thumbnail_url" db:"thumbnail_url"`
	ParsedVideoDir      string                `json:"-" db:"-"`
	VideoPath           string                `json:"video_path" db:"video_path"`
	ParsedMetaDir       string                `json:"-" db:"-"`
	JSONPath            string                `json:"json_path" db:"json_path"`
	Finished            bool                  `json:"finished" db:"finished"`
	JSONCustomFile      string                `json:"-" db:"-"`
	URL                 string                `json:"url" db:"url"`
	DirectVideoURL      string                `json:"-" db:"-"`
	Title               string                `json:"title" db:"title"`
	Description         string                `json:"description" db:"description"`
	UploadDate          time.Time             `json:"upload_date" db:"upload_date"`
	MetadataMap         map[string]any        `json:"-" db:"-"`
	DownloadStatus      DLStatus              `json:"download_status" db:"download_status"`
	CreatedAt           time.Time             `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at" db:"updated_at"`
	MoveOpOutputDir     string                `json:"-" db:"-"`
	Ignored             bool                  `json:"ignored" db:"ignored"`
	FilteredMetaOps     []FilteredMetaOps     `json:"-" db:"-"` // Per-video filtered meta operations
	FilteredFilenameOps []FilteredFilenameOps `json:"-" db:"-"` // Per-video filtered filename operations
}

// StoreFilenamesFromMetarr stores filenames after Metarr completion.
func (v *Video) StoreFilenamesFromMetarr(cmdOut string) {
	lines := strings.Split(cmdOut, "\n")
	if len(lines) < 2 {
		logger.Pl.E("Metarr did not return expected lines for video %q. Got: %v", v.URL, lines)
		return
	}

	// Find lines
	for _, l := range lines {
		if l == "" {
			continue
		}

		if after, ok := strings.CutPrefix(l, "final video path: "); ok {
			mVPath := after
			if mVPath != "" {
				if _, err := os.Stat(mVPath); err != nil {
					logger.Pl.E("Got invalid video path %q from Metarr: %v", mVPath, err)
				}
				v.VideoPath = mVPath
			}
		}

		if after, ok := strings.CutPrefix(l, "final json path: "); ok {
			mJPath := after
			if mJPath != "" {
				if _, err := os.Stat(mJPath); err != nil {
					logger.Pl.E("Got invalid JSON path %q from Metarr: %v", mJPath, err)
				}
				v.JSONPath = mJPath
			}
		}
	}
	logger.Pl.S("Video %q got filenames from Metarr:\n\nVideo: %q\nJSON: %q", v.URL, v.VideoPath, v.JSONPath)
}

// MarkVideoAsIgnored marks the video as completed and ignored.
func (v *Video) MarkVideoAsIgnored() {
	if v.Title == "" && v.URL != "" {
		v.Title = v.URL
	}
	v.DownloadStatus.Status = consts.DLStatusIgnored
	v.DownloadStatus.Percent = 0.0
	v.DownloadStatus.Error = nil
	v.Ignored = true
}

// MarkVideoAsCompleted marks the video with the finished status.
func (v *Video) MarkVideoAsCompleted() {
	v.DownloadStatus.Status = consts.DLStatusCompleted
	v.DownloadStatus.Percent = 100.0
	v.DownloadStatus.Error = nil
	v.Finished = true
}

// Notification holds notification data for channels.
type Notification struct {
	ChannelID int64
	ChannelURL,
	NotifyURL,
	Name string
}
