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
	LoadErr              error     // Why its saved settings failed validation on load: it can be listed, fixed or deleted, but not run.
}

// DefaultCrawlFreq is the crawl frequency in minutes for a channel without one.
const DefaultCrawlFreq = 30

// URLCrawlFreq returns a URL's crawl frequency setting in minutes: its own, else the channel's.
func (c *Channel) URLCrawlFreq(cu *ChannelURL) int {
	if cu.ChanURLSettings != nil && cu.ChanURLSettings.CrawlFreq > 0 {
		return cu.ChanURLSettings.CrawlFreq
	}
	return c.ChanSettings.CrawlFreq
}

// DueURLs returns the crawlable URLs (not manual or paused) whose crawl frequency, plus extra, has passed since their
// last crawl, and the wait until the next is due (-1 if none). A frequency of 0 or less is the default frequency on
// the web scheduler (scheduled), and every run in the terminal.
func (c *Channel) DueURLs(now time.Time, extra time.Duration, scheduled bool) (due []*ChannelURL, next time.Duration) {
	next = -1
	for _, cu := range c.URLModels {
		if cu.IsManual || cu.Paused() {
			continue
		}
		freq := c.URLCrawlFreq(cu)
		if freq < 0 || (scheduled && freq == 0) {
			freq = DefaultCrawlFreq
		}
		if freq == 0 { // Crawled on every terminal run.
			due = append(due, cu)
			continue
		}
		wait := time.Duration(freq)*time.Minute + extra - now.Sub(cu.LastScan)
		if wait <= 0 {
			due = append(due, cu)
		} else if next < 0 || wait < next {
			next = wait
		}
	}
	return due, next
}

// AutoDownloads reports whether new videos found at cu download without the user choosing them.
func (c *Channel) AutoDownloads(cu *ChannelURL) bool {
	if cu != nil && cu.ChanURLSettings != nil && cu.ChanURLSettings.AutoDownload != nil {
		return *cu.ChanURLSettings.AutoDownload
	}
	return c.ChanSettings != nil && c.ChanSettings.AutoDownload != nil && *c.ChanSettings.AutoDownload
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

// URLByID returns the channel's URL model with the given ID, or nil.
func (c *Channel) URLByID(id int64) *ChannelURL {
	for _, cu := range c.URLModels {
		if cu.ID == id {
			return cu
		}
	}
	return nil
}
