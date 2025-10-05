// Package models holds structs for modelling data, e.g. Channel data, Video data, etc.
package models

import (
	"net/http"
	"time"
)

type Site struct {
	ID        int64      `db:"id"`
	Domain    string     `db:"domain"` // e.g. youtube.com
	Name      string     `db:"name"`
	Channels  []*Channel `json:"channels"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Channel is the top level model for a channel.
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

// ChannelURL contains fields relating to a channel's URL.
//
// Matches the order of the DB table, do not alter.
type ChannelURL struct {
	ID         int64  `db:"id"`
	URL        string `db:"url"`
	Videos     []*Video
	Username   string `db:"username"`
	Password   string `db:"password"`
	LoginURL   string `db:"login_url"`
	CookiePath string
	Cookies    []*http.Cookie
	LastScan   time.Time `db:"last_scan"`
	CreatedAt  time.Time `db:"created_at"`
	UpdatedAt  time.Time `db:"updated_at"`
}

// Video contains fields relating to a video, and a pointer to the channel it belongs to.
//
// Matches the order of the DB table, do not alter.
type Video struct {
	ID                  int64
	ChannelID           int64
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

// Notifications holds notification data for channels.
type Notification struct {
	ChannelID  int64
	ChannelURL string
	NotifyURL  string
	Name       string
}
