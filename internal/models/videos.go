package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
)

// Video is a video in a channel, with its metadata, files and download state.
type Video struct {
	// Identity.
	ID           int64  `json:"id" db:"id"`
	URL          string `json:"url" db:"url"`
	ChannelID    int64  `json:"channel_id" db:"channel_id"`
	ChannelName  string `json:"channel_name,omitempty" db:"-"` // Its channel's name, set when loaded.
	ChannelURLID int64  `json:"channel_url_id" db:"channel_url_id"`
	ChannelURL   string `json:"channel_url" db:"channel_url"`

	// In the JSON at the top level.
	Metadata VideoMetadata `json:"-" db:"-"`
	Files    VideoFiles    `json:"-" db:"-"`

	// State.
	DownloadStatus DLStatus            `json:"download_status" db:"download_status"`
	Finished       bool                `json:"finished" db:"finished"`
	Ignored        bool                `json:"ignored" db:"ignored"`
	MetarrStatus   consts.MetarrStatus `json:"metarr_status" db:"metarr_status"`
	MetarrError    string              `json:"metarr_error,omitempty" db:"metarr_error"` // Why Metarr last failed.
	CreatedAt      time.Time           `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at" db:"updated_at"`

	// Download As.
	Overrides       *VideoOverrides `json:"overrides,omitempty" db:"overrides"` // Chosen with Download As, in place of the channel URL's settings.
	Customized      bool            `json:"customized,omitempty" db:"-"`        // Has Download As options, which its downloads and post-processing use.
	CustomThumbnail bool            `json:"custom_thumbnail,omitempty" db:"-"`  // Download As set one, shown from /api/v1/videos/{id}/thumbnail.

	// This run's processing.
	FilteredMetaOps     []FilteredMetaOps     `json:"-" db:"-"` // Filtered meta ops matching this video.
	FilteredFilenameOps []FilteredFilenameOps `json:"-" db:"-"` // Filtered filename ops matching this video.
	SkipFilters         bool                  `json:"-" db:"-"` // Chosen by the user, so download filters don't apply.
	SkipMetarr          SkipMetarr            `json:"-" db:"-"`
}

// VideoMetadata holds the metadata elements of a video.
type VideoMetadata struct {
	Raw            map[string]any `json:"-" db:"-"` // The metadata file's fields.
	Description    string         `json:"description" db:"description"`
	DirectVideoURL string         `json:"-" db:"-"`
	Duration       int            `json:"duration" db:"duration"` // Seconds, if the crawl listing gave it.
	Filesize       int64          `json:"filesize" db:"filesize"` // Size in bytes.
	ThumbnailURL   string         `json:"thumbnail_url" db:"thumbnail_url"`
	Title          string         `json:"title" db:"title"`
	UploadDate     time.Time      `json:"upload_date" db:"upload_date"`
}

// VideoFiles holds the file paths of a video.
type VideoFiles struct {
	VideoFilePath   string `json:"video_path" db:"video_path"`
	VideoDir        string `json:"-" db:"-"`
	JSONFilePath    string `json:"json_path" db:"json_path"`
	JSONDir         string `json:"-" db:"-"`
	JSONCustomFile  string `json:"-" db:"-"`
	MoveOpOutputDir string `json:"-" db:"-"` // Where a move op sends the finished files.
}

// videoFields is a Video without its JSON methods.
type videoFields Video

// videoJSON is a video's JSON, with its metadata and file fields at the top level.
type videoJSON struct {
	*videoFields
	*VideoMetadata
	*VideoFiles
}

// jsonFields returns the video's JSON fields, pointing into the video.
func (v *Video) jsonFields() videoJSON {
	return videoJSON{(*videoFields)(v), &v.Metadata, &v.Files}
}

// MarshalJSON writes the video with its metadata and file fields at the top level.
func (v Video) MarshalJSON() ([]byte, error) {
	return json.Marshal(v.jsonFields())
}

// UnmarshalJSON reads a video written by MarshalJSON.
func (v *Video) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, new(v.jsonFields()))
}

// VideoOverrides holds a video's directories, yt-dlp options and Metarr args chosen with Download As, used instead of its
// channel URL's (and global) ones.
type VideoOverrides struct {
	Filename       string `json:"filename,omitempty"`
	VideoDir       string `json:"video_dir,omitempty"` // Blank uses the channel URL's.
	JSONDir        string `json:"json_dir,omitempty"`  // Blank uses the channel URL's.
	Thumbnail      string `json:"thumbnail,omitempty"` // An image file or URL embedded in place of the metadata's.
	StripThumbnail bool   `json:"strip_thumbnail,omitempty"`

	// yt-dlp options, blank ones using the channel URL's.
	YtdlpOutputExt            string   `json:"ytdlp_output_ext,omitempty"`
	MaxFilesize               string   `json:"max_filesize,omitempty"`
	MaxResolution             string   `json:"max_resolution,omitempty"`
	ExtraYTDLPVideoArgs       string   `json:"extra_ytdlp_video_args,omitempty"`
	YTDLPPreferredVideoCodecs []string `json:"ytdlp_preferred_video_codecs,omitempty"`
	YTDLPPreferredAudioCodecs []string `json:"ytdlp_preferred_audio_codecs,omitempty"`

	// Ops, transcode codecs, encoder args and extra FFmpeg args replace the channel URL's. Other blank fields use the channel URL's.
	MetarrArgs

	// The channel URL's ops when these were saved, so its lines in them can be refreshed. Nil keeps the ops as saved.
	ChannelURLOps *OpLines `json:"channel_url_ops,omitempty"`
}

// OpLines holds the Download As dialog's op boxes, one op per line.
type OpLines struct {
	MetaOps             string `json:"meta_ops,omitempty"`
	FilteredMetaOps     string `json:"filtered_meta_ops,omitempty"`
	FilenameOps         string `json:"filename_ops,omitempty"`
	FilteredFilenameOps string `json:"filtered_filename_ops,omitempty"`
}

// OverrideLines holds a video's overrides as edited in the Download As dialog, with ops and codecs newline-separated.
type OverrideLines struct {
	Filename                  string `json:"filename"`
	VideoDir                  string `json:"video_dir"`
	JSONDir                   string `json:"json_dir"`
	OutputDir                 string `json:"output_dir"`
	MetaOps                   string `json:"meta_ops"`
	FilteredMetaOps           string `json:"filtered_meta_ops"`
	FilenameOps               string `json:"filename_ops"`
	FilteredFilenameOps       string `json:"filtered_filename_ops"`
	Thumbnail                 string `json:"thumbnail"`
	StripThumbnail            bool   `json:"strip_thumbnail"`
	YtdlpOutputExt            string `json:"ytdlp_output_ext"`
	MaxFilesize               string `json:"max_filesize"`
	MaxResolution             string `json:"max_resolution"`
	ExtraYTDLPVideoArgs       string `json:"extra_ytdlp_video_args"`
	YTDLPPreferredVideoCodecs string `json:"ytdlp_preferred_video_codecs"`
	YTDLPPreferredAudioCodecs string `json:"ytdlp_preferred_audio_codecs"`
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

// AvailableVideo is a video awaiting the user's choice to download it.
type AvailableVideo struct {
	Video
	CrawlRetries
}

// CrawlRetries is how an available video's crawls retry its download.
type CrawlRetries struct {
	RetriesOnCrawl     bool `json:"retries_on_crawl,omitempty"` // A failed video the channel URL's next crawl downloads again.
	CrawlDLRetries     int  `json:"-"`                          // Crawl retries it has used, all of them once it's left to the user.
	CrawlDLRetriesLeft int  `json:"crawl_dl_retries_left,omitempty"`
}

// availableVideoJSON is an available video's JSON, as Video's JSON methods would drop its crawl retries.
type availableVideoJSON struct {
	videoJSON
	*CrawlRetries
}

// MarshalJSON writes the video with its crawl retries.
func (v AvailableVideo) MarshalJSON() ([]byte, error) {
	return json.Marshal(availableVideoJSON{v.jsonFields(), &v.CrawlRetries})
}

// UnmarshalJSON reads an available video written by MarshalJSON.
func (v *AvailableVideo) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &availableVideoJSON{v.jsonFields(), &v.CrawlRetries})
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
				v.Files.VideoFilePath = after
				gotVideo = true
			}
		}

		// JSON path, empty once the metafile was purged or is gone.
		if after, ok := strings.CutPrefix(l, "final json path: "); ok {
			v.Files.JSONFilePath = after
		}
	}

	// Check video path was obtained.
	if !gotVideo {
		return fmt.Errorf("metarr did not return the final video path for %q. Got lines: %v", v.URL, lines)
	}

	logger.Pl.I("Video %q got file paths from Metarr:\n\nVideo: %q\nJSON: %q", v.URL, v.Files.VideoFilePath, v.Files.JSONFilePath)
	return nil
}

// MarkVideoAsIgnored marks the video as ignored, so crawls skip it without downloading.
func (v *Video) MarkVideoAsIgnored() {
	if v.Metadata.Title == "" && v.URL != "" {
		v.Metadata.Title = v.URL
	}
	v.DownloadStatus.Status = consts.DLStatusIgnored
	v.DownloadStatus.Percent = 0.0
	v.DownloadStatus.Error = nil
	v.Ignored = true
}

// MarkVideoAsAvailable marks the video as awaiting the user's choice to download it.
func (v *Video) MarkVideoAsAvailable() {
	if v.Metadata.Title == "" && v.URL != "" {
		v.Metadata.Title = v.URL
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
