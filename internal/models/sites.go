// Package models holds structs for modelling data, e.g. Channel data, Video data, etc.
package models

import (
	"net/http"
	"time"
)

// Site is not yet implemented.
type Site struct {
	ID       int64      `db:"id"`
	Domain   string     `db:"domain"` // e.g. youtube.com ??? Or just make empty of domain to hold channels ???
	Name     string     `db:"name"`
	Channels []*Channel `json:"channels"`
	CreatedAt,
	UpdatedAt time.Time
}

// Channel is the top level model for a channel.
//
// Contains ChannelURL models and top level configuration.
type Channel struct {
	ID                int64 `db:"id"`
	URLModels         []*ChannelURL
	Name              string           `db:"name"`
	ChanSettings      *ChannelSettings `json:"settings" db:"settings"`
	ChanMetarrArgs    *MetarrArgs      `json:"metarr" db:"metarr"`
	UpdatedFromConfig bool
	LastScan          time.Time `db:"last_scan"`
	CreatedAt         time.Time `db:"created_at"`
	UpdatedAt         time.Time `db:"updated_at"`
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
	return timeSince >= time.Duration(c.ChanSettings.CrawlFreq)*time.Minute
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

// NeedsAuth checks if this channel URL has details for authorization.
//
// Returns true if both a username and login URL are provided. Password may be blank.
func (cu *ChannelURL) NeedsAuth() bool {
	return cu.Username != "" && cu.LoginURL != ""
}

// ChannelURL contains fields relating to a channel's URL.
//
// Matches the order of the DB table, do not alter.
type ChannelURL struct {
	ID         int64  `db:"id"`
	URL        string `db:"url"`
	Videos     []*Video
	Username   string         `db:"username"`
	Password   string         `db:"password"`
	LoginURL   string         `db:"login_url"`
	CookiePath string         `db:"-" json:"-"`
	Cookies    []*http.Cookie `db:"-" json:"-"`
	IsManual   bool           `db:"is_manual"`
	LastScan   time.Time      `db:"last_scan"`
	CreatedAt  time.Time      `db:"created_at"`
	UpdatedAt  time.Time      `db:"updated_at"`
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
	ID,
	ChannelID int64
	ChannelURLID        int64
	ChannelURL          string
	ParsedVideoDir      string
	VideoPath           string `db:"video_path"`
	ParsedJSONDir       string
	JSONPath            string `db:"json_path"`
	Finished            bool   `db:"finished"`
	JSONCustomFile      string
	URL                 string `db:"url"`
	DirectVideoURL      string
	Title               string           `db:"title"`
	Description         string           `db:"description"`
	UploadDate          time.Time        `db:"upload_date"`
	MetadataMap         map[string]any   `db:"-"`
	Settings            *ChannelSettings `json:"settings" db:"settings"`
	MetarrArgs          *MetarrArgs      `json:"metarr" db:"metarr"`
	DownloadStatus      DLStatus         `json:"download_status" db:"download_status"`
	CreatedAt           time.Time        `db:"created_at"`
	UpdatedAt           time.Time        `db:"updated_at"`
	BaseDomain          string
	BaseDomainWithProto string
	MoveOpOutputDir     string
	MoveOpChannelURL    string
	WasSkipped          bool
}

// Notification holds notification data for channels.
type Notification struct {
	ChannelID int64
	ChannelURL,
	NotifyURL,
	Name string
}
