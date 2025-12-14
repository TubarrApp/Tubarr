package models

import (
	"fmt"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
)

// Video contains fields relating to a video, and a pointer to the channel it belongs to.
type Video struct {
	ID                  int64                 `json:"id" db:"id"`
	ChannelID           int64                 `json:"channel_id" db:"channel_id"`
	ChannelURLID        int64                 `json:"channel_url_id" db:"channel_url_id"`
	ThumbnailURL        string                `json:"thumbnail_url" db:"thumbnail_url"`
	VideoFilePath       string                `json:"video_path" db:"video_path"`
	VideoDir            string                `json:"-" db:"-"`
	JSONFilePath        string                `json:"json_path" db:"json_path"`
	JSONDir             string                `json:"-" db:"-"`
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
func (v *Video) StoreFilenamesFromMetarr(cmdOut string) error {
	lines := strings.Split(cmdOut, "\n")
	if len(lines) == 0 {
		return fmt.Errorf("metarr did not return any lines for %q, could not grab final file paths", v.URL)
	}

	// Find file path lines.
	gotVideo := false
	for _, l := range lines {
		if l == "" {
			continue
		}

		// Video path.
		if after, ok := strings.CutPrefix(l, "final video path: "); ok {
			if after != "" {
				v.VideoFilePath = after
				gotVideo = true
			}
		}

		// JSON path.
		if after, ok := strings.CutPrefix(l, "final json path: "); ok {
			if after != "" {
				v.JSONFilePath = after
			}
		}
	}

	// Check video path was obtained.
	if !gotVideo {
		return fmt.Errorf("metarr did not return the final video path for %q. Got lines: %v", v.URL, lines)
	}

	logger.Pl.S("Video %q got file paths from Metarr:\n\nVideo: %q\nJSON: %q", v.URL, v.VideoFilePath, v.JSONFilePath)
	return nil
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

// MarkVideoAsComplete marks the video with the finished status.
func (v *Video) MarkVideoAsComplete() {
	v.DownloadStatus.Status = consts.DLStatusCompleted
	v.DownloadStatus.Percent = 100.0
	v.DownloadStatus.Error = nil
	v.Finished = true
}
