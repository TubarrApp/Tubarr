package models

import (
	"database/sql"
)

// Store allows access to the main store repo methods.
type Store interface {
	GetChannelStore() ChannelStore
	GetVideoStore() VideoStore
}

// ChannelStore allows access to channel repo methods.
type ChannelStore interface {
	AddChannel(c *Channel) (int64, error)
	CrawlChannel(key, val string, s Store) error
	CrawlChannelIgnore(key, val string, s Store) error
	AddURLToIgnore(channelID int64, ignoreURL string) error
	DeleteChannel(key, val string) error
	ListChannels() (channels []*Channel, err error, hasRows bool)
	LoadGrabbedURLs(c *Channel) (urls []string, err error)
	UpdateChannelRow(key, val, col, newVal string) error
	UpdateLastScan(channelID int64) error
	UpdateChannelSettings(key, val string, updateFn func(*ChannelSettings) error) error
	UpdateCrawlFrequency(key, val string, newFreq int) error
	UpdateExternalDownloader(key, val, downloader, args string) error
	UpdateConcurrencyLimit(key, val string, newConc int) error
	UpdateChannelEntry(chanKey, chanVal, updateKey, updateVal string) error
	UpdateMetarrOutputDir(key, val string, outDir string) error
	UpdateMaxFilesize(key, val, maxSize string) error
	GetID(key, val string) (int64, error)
	AddNotifyURL(id int64, notifyName, notifyURL string) error
	GetNotifyURLs(id int64) ([]string, error)
	GetDB() *sql.DB
}

// VideoStore allows access to video repo methods.
type VideoStore interface {
	AddVideo(v *Video) (int64, error)
	DeleteVideo(key, val string, chanID int64) error
	UpdateVideo(v *Video) error
	GetDB() *sql.DB
}
