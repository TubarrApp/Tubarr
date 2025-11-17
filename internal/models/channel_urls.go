package models

import (
	"net/http"
	"time"
)

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
