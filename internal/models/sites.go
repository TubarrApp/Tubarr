// Package models holds structs for modelling data, e.g. Channel data, Video data, etc.
package models

import (
	"time"
)

// Channel contains fields relating to a channel.
//
// Matches the order of the DB table, do not alter.
type Channel struct {
	ID         int64 `db:"id"`
	URLs       []string
	Name       string          `db:"name"`
	VideoDir   string          `db:"video_directory"`
	JSONDir    string          `db:"json_directory"`
	Settings   ChannelSettings `json:"settings" db:"settings"`
	MetarrArgs MetarrArgs      `json:"metarr" db:"metarr"`
	LastScan   time.Time       `db:"last_scan"`
	Paused     bool            `db:"paused"`
	CreatedAt  time.Time       `db:"created_at"`
	UpdatedAt  time.Time       `db:"updated_at"`
}

// Video contains fields relating to a video, and a pointer to the channel it belongs to..
//
// Matches the order of the DB table, do not alter.
type Video struct {
	ID                  int64
	ChannelID           int64  `db:"channel_id"`
	VideoDir            string `db:"video_directory"`
	VideoPath           string `db:"video_path"`
	JSONDir             string `db:"json_directory"`
	JSONPath            string `db:"json_path"`
	Finished            bool   `db:"finished"`
	JSONCustomFile      string
	URL                 string `db:"url"`
	DirectVideoURL      string
	Title               string          `db:"title"`
	Description         string          `db:"description"`
	UploadDate          time.Time       `db:"upload_date"`
	MetadataMap         map[string]any  `db:"-"`
	Channel             *Channel        `db:"-"`
	Settings            ChannelSettings `json:"settings" db:"settings"`
	MetarrArgs          MetarrArgs      `json:"metarr" db:"metarr"`
	DownloadStatus      DLStatus        `json:"download_status" db:"download_status"`
	CreatedAt           time.Time       `db:"created_at"`
	UpdatedAt           time.Time       `db:"updated_at"`
	Username            string
	Password            string
	LoginURL            string
	CookiePath          string
	BaseDomain          string
	BaseDomainWithProto string
}
