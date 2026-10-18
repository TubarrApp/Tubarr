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
	ChannelURL          string                `json:"channel_url" db:"channel_url"`
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
	Duration            int                   `json:"duration" db:"duration"` // Seconds, if the crawl listing gave it.
	MetadataMap         map[string]any        `json:"-" db:"-"`
	DownloadStatus      DLStatus              `json:"download_status" db:"download_status"`
	CreatedAt           time.Time             `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at" db:"updated_at"`
	MoveOpOutputDir     string                `json:"-" db:"-"`
	Ignored             bool                  `json:"ignored" db:"ignored"`
	MetarrStatus        consts.MetarrStatus   `json:"metarr_status" db:"metarr_status"`
	FilteredMetaOps     []FilteredMetaOps     `json:"-" db:"-"` // Per-video filtered meta operations
	FilteredFilenameOps []FilteredFilenameOps `json:"-" db:"-"` // Per-video filtered filename operations
	SkipFilters         bool                  `json:"-" db:"-"` // Chosen by the user, so download filters don't apply.
	SkipMetarr          SkipMetarr            `json:"-" db:"-"`
	SkipDownloads       SkipDownloads         `json:"-" db:"-"`
}

// SkipMetarr skips Metarr processing with a given reason.
type SkipMetarr struct {
	Skip   bool
	Reason SkipMetarrReasons
}
type SkipMetarrReasons string

const (
	SkipMetarrNoPath                         SkipMetarrReasons = "metarr isn't on $PATH or won't run"
	SkipMetarrMissingFilenameOpsFile         SkipMetarrReasons = "filename ops path is set, but the file can't be read or parsed"
	SkipMetarrMissingMetaOpsFile             SkipMetarrReasons = "meta ops path is set, but the file can't be read or parsed"
	SkipMetarrMissingFilteredMetaOpsFile     SkipMetarrReasons = "filtered meta ops path is set, but the file can't be read or parsed"
	SkipMetarrMissingFilteredFilenameOpsFile SkipMetarrReasons = "filtered filename ops path is set, but the file can't be read or parsed"
)

// SkipDownloads skips any JSON or video downloads with a given reason.
type SkipDownloads struct {
	Skip   bool
	Reason SkipDownloadReasons
}
type SkipDownloadReasons string

const (
	SkipDLMissingFilterFile SkipDownloadReasons = "filter file path is set, but file appears to be unreachable"
)

// AvailableVideo is a video awaiting the user's choice to download it, with its channel's name.
type AvailableVideo struct {
	Video
	ChannelName    string `json:"channel_name"`
	RetriesOnCrawl bool   `json:"retries_on_crawl,omitempty"` // A failed or canceled video the channel URL's next crawl downloads again.
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

	logger.Pl.I("Video %q got file paths from Metarr:\n\nVideo: %q\nJSON: %q", v.URL, v.VideoFilePath, v.JSONFilePath)
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

// MarkVideoAsAvailable marks the video as awaiting the user's choice to download it.
func (v *Video) MarkVideoAsAvailable() {
	if v.Title == "" && v.URL != "" {
		v.Title = v.URL
	}
	v.DownloadStatus.Status = consts.DLStatusAvailable
	v.DownloadStatus.Percent = 0.0
	v.DownloadStatus.Error = nil
}

// MarkVideoAsComplete marks the video with the finished status.
func (v *Video) MarkVideoAsComplete() {
	v.DownloadStatus.Status = consts.DLStatusCompleted
	v.DownloadStatus.Percent = 100.0
	v.DownloadStatus.Error = nil
	v.Finished = true
}
