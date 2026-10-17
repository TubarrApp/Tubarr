// Package models holds structs for modelling data, e.g. Channel data, Video data, etc.
package models

import (
	"time"
)

// Channel is the top level model for a channel.
//
// Contains ChannelURL models and top level configuration.
type Channel struct {
	ID                   int64 `db:"id"`
	URLModels            []*ChannelURL
	Name                 string      `db:"name"`
	ChannelConfigFile    string      `db:"channel_config_file"`
	ChanSettings         *Settings   `db:"settings"`
	ChanMetarrArgs       *MetarrArgs `db:"metarr"`
	UpdatedFromConfig    bool
	LastScan             time.Time `db:"last_scan"`
	CreatedAt            time.Time `db:"created_at"`
	UpdatedAt            time.Time `db:"updated_at"`
	NewVideoNotification bool      `db:"new_video_notification"`
}

// DefaultCrawlFreq is the crawl frequency in minutes for a channel without one.
const DefaultCrawlFreq = 30

// GetCrawlFreq returns the program's crawl frequency (-1 is unset). 0 means every terminal run.
func (c *Channel) GetCrawlFreq() int {
	if c.ChanSettings.CrawlFreq < 0 {
		return DefaultCrawlFreq
	}
	return c.ChanSettings.CrawlFreq
}

// ScheduledCrawlFreq returns the web scheduler's crawl frequency in minutes, using the default for 0 or less (e.g. a
// channel added from the terminal without --crawl-freq), which would otherwise be crawled constantly.
func (c *Channel) ScheduledCrawlFreq() int {
	if c.ChanSettings.CrawlFreq <= 0 {
		return DefaultCrawlFreq
	}
	return c.ChanSettings.CrawlFreq
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

// AutoDownloads reports whether new videos found at cu download without the user choosing them.
func (c *Channel) AutoDownloads(cu *ChannelURL) bool {
	if cu != nil && cu.ChanURLSettings != nil && cu.ChanURLSettings.AutoDownload != nil {
		return *cu.ChanURLSettings.AutoDownload
	}
	return c.ChanSettings == nil || c.ChanSettings.AutoDownload == nil || *c.ChanSettings.AutoDownload
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
