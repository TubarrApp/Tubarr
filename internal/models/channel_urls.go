package models

import (
	"net/http"
	"time"
)

// ChannelURL contains fields relating to a channel's URL.
type ChannelURL struct {
	ID                int64                   `json:"id" db:"id"`
	URL               string                  `json:"url" db:"url"`
	Name              string                  `json:"name" db:"name"`
	ChannelID         int64                   `json:"channel_id" db:"channel_id"`
	ChannelName       string                  `json:"channel_name,omitempty" db:"-"` // Its channel's name, set when loaded.
	Videos            []*Video                `json:"videos,omitempty"`
	Login             ChannelURLAccessDetails `json:"login"`
	Session           ChannelURLSession       `json:"-" db:"-"`
	IsManual          bool                    `json:"is_manual" db:"is_manual"`
	ChanURLSettings   *Settings               `json:"settings,omitempty" db:"settings"`
	ChanURLMetarrArgs *MetarrArgs             `json:"metarr,omitempty" db:"metarr"`
	LastScan          time.Time               `json:"last_scan" db:"last_scan"`
	CreatedAt         time.Time               `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at" db:"updated_at"`
	ProfileID         int64                   `json:"profile_id" db:"profile_id"`
	Profile           *Profile                `json:"-" db:"-"`
	Levels            []SettingsLevel         `json:"-" db:"-"` // Where its filters and op lists come from. Set when loaded merged with its parents.
}

// ChannelURLAccessDetails holds a channel URL's login details.
type ChannelURLAccessDetails struct {
	Username          string `json:"username" db:"username"`
	Password          string `json:"-" db:"-"`
	EncryptedPassword string `json:"-" db:"password"`
	LoginURL          string `json:"login_url" db:"login_url"`
}

// ChannelURLSession holds a channel URL's cookies and FlareSolverr solution for this run.
type ChannelURLSession struct {
	CookiePath   string
	Cookies      []*http.Cookie
	FlareSolverr *FlareSolverrSolution
}

// NeedsAuth checks if this channel URL has details for authorization.
//
// Returns true if both a username and login URL are provided. Password may be blank.
func (cu *ChannelURL) NeedsAuth() bool {
	return cu.Login.Username != "" && cu.Login.LoginURL != ""
}

// Paused reports whether the channel URL's own settings pause it.
func (cu *ChannelURL) Paused() bool {
	return cu.ChanURLSettings != nil && cu.ChanURLSettings.Paused
}
