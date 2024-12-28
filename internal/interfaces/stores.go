// Package interfaces holds all interface definitions.
package interfaces

import (
	"context"
	"database/sql"

	"tubarr/internal/models"
)

// Store allows access to the main store repo methods.
type Store interface {
	ChannelStore() ChannelStore
	DownloadStore() DownloadStore
	VideoStore() VideoStore
}

// ChannelStore allows access to channel repo methods.
type ChannelStore interface {
	AddAuth(channelID int64, username, password, loginURL string) error
	AddChannel(c *models.Channel) (int64, error)
	AddNotifyURL(id int64, notifyName, notifyURL string) error
	AddURLToIgnore(channelID int64, ignoreURL string) error
	CrawlChannel(key, val string, s Store, ctx context.Context) error
	CrawlChannelIgnore(key, val string, s Store, ctx context.Context) error
	DeleteChannel(key, val string) error
	DeleteVideoURLs(channelID int64, urls []string) error
	DeleteNotifyURLs(channelID int64, urls, names []string) error
	FetchAllChannels() (channels []*models.Channel, err error, hasRows bool)
	FetchChannel(id int64) (c *models.Channel, err error, hasRows bool)
	GetAuth(channelID int64) (username, password, loginURL string, err error)
	GetDB() *sql.DB
	GetID(key, val string) (int64, error)
	GetNotifyURLs(id int64) ([]string, error)
	LoadGrabbedURLs(c *models.Channel) (urls []string, err error)
	UpdateChannelEntry(chanKey, chanVal, updateKey, updateVal string) error
	UpdateChannelMetarrArgsJSON(key, val string, updateFn func(*models.MetarrArgs) error) (int64, error)
	UpdateChannelSettingsJSON(key, val string, updateFn func(*models.ChannelSettings) error) (int64, error)
	UpdateChannelRow(key, val, col, newVal string) error
	UpdateLastScan(channelID int64) error
}

type DownloadStore interface {
	GetDB() *sql.DB
	SetDownloadStatus(v *models.Video) error
	UpdateDownloadStatuses(ctx context.Context, updates []models.StatusUpdate) error
}

// VideoStore allows access to video repo methods.
type VideoStore interface {
	AddVideo(v *models.Video) (int64, error)
	AddVideos(videos []*models.Video, c *models.Channel) ([]*models.Video, []error)
	GetDB() *sql.DB
	DeleteVideo(key, val string, chanID int64) error
	UpdateVideo(v *models.Video) error
}
