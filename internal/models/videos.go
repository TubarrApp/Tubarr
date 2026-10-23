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
	CustomThumbnail     bool                  `json:"custom_thumbnail,omitempty" db:"-"` // Download As set one, shown from /api/v1/videos/{id}/thumbnail.
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
	Filesize            int64                 `json:"filesize" db:"filesize"` // Size in bytes.
	MetadataMap         map[string]any        `json:"-" db:"-"`
	DownloadStatus      DLStatus              `json:"download_status" db:"download_status"`
	CreatedAt           time.Time             `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at" db:"updated_at"`
	MoveOpOutputDir     string                `json:"-" db:"-"`
	Ignored             bool                  `json:"ignored" db:"ignored"`
	MetarrStatus        consts.MetarrStatus   `json:"metarr_status" db:"metarr_status"`
	MetarrError         string                `json:"metarr_error,omitempty" db:"metarr_error"` // Why Metarr last failed.
	FilteredMetaOps     []FilteredMetaOps     `json:"-" db:"-"`                                 // Per-video filtered meta operations
	FilteredFilenameOps []FilteredFilenameOps `json:"-" db:"-"`                                 // Per-video filtered filename operations
	SkipFilters         bool                  `json:"-" db:"-"`                                 // Chosen by the user, so download filters don't apply.
	SkipMetarr          SkipMetarr            `json:"-" db:"-"`
	SkipDownloads       SkipDownloads         `json:"-" db:"-"`
	Overrides           *VideoOverrides       `json:"overrides,omitempty" db:"overrides"` // Chosen with Download As, in place of the channel URL's settings.
}

// VideoOverrides holds a video's directories and Metarr args chosen with Download As, used instead of its channel URL's
// (and global) ones.
type VideoOverrides struct {
	Filename       string `json:"filename,omitempty"`
	VideoDir       string `json:"video_dir,omitempty"` // Blank uses the channel URL's.
	JSONDir        string `json:"json_dir,omitempty"`  // Blank uses the channel URL's.
	Thumbnail      string `json:"thumbnail,omitempty"` // An image file or URL embedded in place of the metadata's.
	StripThumbnail bool   `json:"strip_thumbnail,omitempty"`
	MetarrArgs            // Ops replace the channel URL's. Other blank fields use the channel URL's.
}

// OverrideLines holds a video's overrides as edited in the Download As dialog, with ops newline-separated.
type OverrideLines struct {
	Filename            string `json:"filename"`
	VideoDir            string `json:"video_dir"`
	JSONDir             string `json:"json_dir"`
	OutputDir           string `json:"output_dir"`
	MetaOps             string `json:"meta_ops"`
	FilteredMetaOps     string `json:"filtered_meta_ops"`
	FilenameOps         string `json:"filename_ops"`
	FilteredFilenameOps string `json:"filtered_filename_ops"`
	Thumbnail           string `json:"thumbnail"`
	StripThumbnail      bool   `json:"strip_thumbnail"`
}

// SkipMetarr skips Metarr processing with a given reason.
type SkipMetarr struct {
	Skip   bool
	Reason SkipMetarrReasons
}

// SkipMetarrReasons are reasons the Metarr step was skipped.
type SkipMetarrReasons string

// SkipMetarrReasons strings.
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

// SkipDownloadReasons are reasons a download was skipped.
type SkipDownloadReasons string

// SkipDownloadReasons strings.
const (
	SkipDLMissingFilterFile SkipDownloadReasons = "filter file path is set, but file appears to be unreachable"
)

// AvailableVideo is a video awaiting the user's choice to download it, with its channel's name.
type AvailableVideo struct {
	Video
	ChannelName        string `json:"channel_name"`
	RetriesOnCrawl     bool   `json:"retries_on_crawl,omitempty"` // A failed video the channel URL's next crawl downloads again.
	CrawlDLRetries     int    `json:"-"`                          // Crawls which have retried it since the user last downloaded it.
	CrawlDLRetriesLeft int    `json:"crawl_dl_retries_left,omitempty"`
}

// ManualVideo is a video URL sent in for a channel, through the channel URL with the given ID (0 for its manual downloads).
type ManualVideo struct {
	URL          string      `json:"url"`
	ChannelURLID int64       `json:"channel_url_id"`
	Auth         *ManualAuth `json:"auth,omitempty"` // Replaces the channel URL's login for this download only.
	Title        string      `json:"-"`              // Known from Available, shown until the metadata arrives.
}

// ManualAuth is login details sent with manual videos, used for their download and never stored.
type ManualAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
	LoginURL string `json:"login_url"`
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

		// Video path, reported as soon as it changes and again once final, so the last line wins.
		for _, prefix := range []string{"current video path: ", "final video path: "} {
			if after, ok := strings.CutPrefix(l, prefix); ok && after != "" {
				v.VideoFilePath = after
				gotVideo = true
			}
		}

		// JSON path, empty once the metafile was purged or is gone.
		if after, ok := strings.CutPrefix(l, "final json path: "); ok {
			v.JSONFilePath = after
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
