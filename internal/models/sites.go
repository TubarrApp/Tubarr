// Package models holds structs for modelling data, e.g. Channel data, Video data, etc.
package models

import (
	"net/http"
	"os"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/utils/logging"
)

// Site is not yet implemented.
type Site struct {
	ID       int64      `db:"id"`
	Domain   string     `db:"domain"` // e.g. tubesite.com ??? Or just make empty of domain to hold channels ???
	Name     string     `db:"name"`
	Channels []*Channel `json:"channels"`
	CreatedAt,
	UpdatedAt time.Time
}

// Channel is the top level model for a channel.
//
// Contains ChannelURL models and top level configuration.
type Channel struct {
	ID                int64         `json:"id" db:"id"`
	URLModels         []*ChannelURL `json:"url_models"`
	Name              string        `json:"name" db:"name"`
	ConfigFile        string        `json:"channel_config_file" mapstructure:"channel-config-file"`
	ChanSettings      *Settings     `json:"settings" db:"settings"`
	ChanMetarrArgs    *MetarrArgs   `json:"metarr" db:"metarr"`
	UpdatedFromConfig bool          `json:"updated_from_config"`
	LastScan          time.Time     `json:"last_scan" db:"last_scan"`
	CreatedAt         time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time     `json:"updated_at" db:"updated_at"`
}

// GetCrawlFreq returns the program's crawl frequency (-1 is unset).
func (c *Channel) GetCrawlFreq() int {
	if c.ChanSettings.CrawlFreq < 0 {
		return 30 // default
	}
	return c.ChanSettings.CrawlFreq
}

// IsBlocked checks if a channel is currently blocked.
//
// Safely returns BotBlocked state.
func (c *Channel) IsBlocked() bool {
	return c.ChanSettings != nil && c.ChanSettings.BotBlocked
}

// ShouldCrawl determines if a channel should be included in the default crawl or not.
//
// Returns true if ChanSettings 'Paused' is true OR the time since last scan exceeds the crawl frequency.
func (c *Channel) ShouldCrawl() bool {
	if c.ChanSettings.Paused {
		return false
	}
	timeSince := time.Since(c.LastScan)
	return timeSince >= time.Duration(c.GetCrawlFreq())*time.Minute
}

// GetURLs returns all URL models for a channel.
func (c *Channel) GetURLs() []string {
	urls := make([]string, 0, len(c.URLModels))
	for _, cu := range c.URLModels {
		if cu.URL != "" {
			urls = append(urls, cu.URL)
		}
	}
	return urls
}

// ChannelURL contains fields relating to a channel's URL.
//
// Matches the order of the DB table, do not alter.
type ChannelURL struct {
	ID                int64          `json:"id" db:"id"`
	URL               string         `json:"url" db:"url"`
	Videos            []*Video       `json:"videos,omitempty"`
	Username          string         `json:"username" db:"username"`
	Password          string         `json:"-" db:"-"`
	EncryptedPassword string         `json:"-" db:"password"`
	LoginURL          string         `json:"login_url" db:"login_url"`
	CookiePath        string         `json:"-" db:"-"`
	Cookies           []*http.Cookie `json:"-" db:"-"`
	IsManual          bool           `json:"is_manual" db:"is_manual"`
	ChanURLSettings   *Settings      `json:"settings,omitempty" db:"settings"`
	ChanURLMetarrArgs *MetarrArgs    `json:"metarr,omitempty" db:"metarr"`
	LastScan          time.Time      `json:"last_scan" db:"last_scan"`
	CreatedAt         time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at" db:"updated_at"`
}

// NeedsAuth checks if this channel URL has details for authorization.
//
// Returns true if both a username and login URL are provided. Password may be blank.
func (cu *ChannelURL) NeedsAuth() bool {
	return cu.Username != "" && cu.LoginURL != ""
}

// ToChannelAccessDetails extracts authentication details from a ChannelURL
func (cu *ChannelURL) ToChannelAccessDetails() *ChannelAccessDetails {
	return &ChannelAccessDetails{
		Username:   cu.Username,
		Password:   cu.Password,
		LoginURL:   cu.LoginURL,
		ChannelURL: cu.URL,
		CookiePath: cu.CookiePath,
	}
}

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
		logging.E("Metarr did not return expected lines for video %q. Got: %v", v.URL, lines)
		return
	}

	mVPath := strings.TrimPrefix(lines[0], "final video path: ")
	if mVPath != "" {
		if _, err := os.Stat(mVPath); err != nil {
			logging.E("Got invalid video path %q from Metarr: %v", mVPath, err)
		}
		v.VideoPath = mVPath
	}

	mJPath := strings.TrimPrefix(lines[1], "final json path: ")
	if mJPath != "" {
		if _, err := os.Stat(mJPath); err != nil {
			logging.E("Got invalid JSON path %q from Metarr: %v", mJPath, err)
		}
		v.JSONPath = mJPath
	}

	logging.S("Video %q got filenames from Metarr:\n\nVideo: %q\nJSON: %q", v.URL, v.VideoPath, v.JSONPath)
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
