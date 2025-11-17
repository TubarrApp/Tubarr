// Package models holds structs for modelling data, e.g. Channel data, Video data, etc.
package models

import (
	"time"
)

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
