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
	DeleteChannel(key, val string) error
	ListChannels() (channels []models.Channel, err error, hasRows bool)
	LoadGrabbedURLs(c *models.Channel) (urls []string, err error)
	UpdateChannelRow(key, val, col, newVal string) error
	UpdateLastScan(channelID int64) error
	GetDB() *sql.DB
}

type VideoStore interface {
	AddVideo(v *models.Video) (int64, error)
	DeleteVideo(key, val string) error
	UpdateVideo(v *models.Video) error
	GetDB() *sql.DB
}
