package interfaces

import (
	"database/sql"
	"tubarr/internal/models"
)

type Store interface {
	GetChannelStore() ChannelStore
	GetVideoStore() VideoStore
}

type ChannelStore interface {
	AddChannel(c *models.Channel) (int64, error)
	CrawlChannel(key, val string, s Store) error
	CrawlChannelIgnore(key, val string, s Store) error
	AddURLToIgnore(channelID int64, ignoreURL string) error
	DeleteChannel(key, val string) error
	ListChannels() (channels []models.Channel, err error, hasRows bool)
	LoadGrabbedURLs(c *models.Channel) (urls []string, err error)
	UpdateChannelRow(key, val, col, newVal string) error
	UpdateLastScan(channelID int64) error
	UpdateChannelSettings(key, val string, updateFn func(*models.ChannelSettings) error) error
	UpdateCrawlFrequency(key, val string, newFreq int) error
	UpdateExternalDownloader(key, val string, downloader, args string) error
	GetID(key, val string) (int64, error)
	AddNotifyURL(id int64, notifyName, notifyURL string) error
	GetNotifyURLs(id int64) ([]string, error)
	GetDB() *sql.DB
}

type VideoStore interface {
	AddVideo(v *models.Video) (int64, error)
	DeleteVideo(key, val string) error
	UpdateVideo(v *models.Video) error
	GetDB() *sql.DB
}
