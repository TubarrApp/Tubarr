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
	AddAuth(chanID int64, authDetails map[string]*models.ChannelAccessDetails) error
	AddChannel(c *models.Channel) (int64, error)
	AddChannelURL(channelID int64, cu *models.ChannelURL, isManual bool) (chanURLID int64, err error)
	AddNotifyURLs(channelID int64, notifications []*models.Notification) error
	AddURLToIgnore(channelID int64, ignoreURL string) error
	CrawlChannel(key, val string, c *models.Channel, s Store, ctx context.Context) error
	CrawlChannelIgnore(key, val string, s Store, ctx context.Context) error
	DeleteChannel(key, val string) error
	DeleteVideoURLs(channelID int64, urls []string) error
	DeleteNotifyURLs(channelID int64, urls, names []string) error
	DownloadVideoURLs(key, val string, c *models.Channel, s Store, videoURLs []string, ctx context.Context) error
	FetchAllChannels() (channels []*models.Channel, err error, hasRows bool)
	FetchChannelModel(key, val string) (*models.Channel, bool, error)
	FetchChannelURLModels(channelID int64) ([]*models.ChannelURL, error)
	GetAuth(channelID int64, url string) (username, password, loginURL string, err error)
	GetDB() *sql.DB
	GetChannelID(key, val string) (int64, error)
	GetNotifyURLs(id int64) ([]*models.Notification, error)
	LoadGrabbedURLs(c *models.Channel) (urls []string, err error)
	UpdateChannelValue(key, val, col string, newVal any) error
	UpdateChannelMetarrArgsJSON(key, val string, updateFn func(*models.MetarrArgs) error) (int64, error)
	UpdateChannelSettingsJSON(key, val string, updateFn func(*models.ChannelSettings) error) (int64, error)
	UpdateLastScan(channelID int64) error
}

type DownloadStore interface {
	GetDB() *sql.DB
	SetDownloadStatus(v *models.Video) error
	UpdateDownloadStatuses(ctx context.Context, updates []models.StatusUpdate) error
}

// VideoStore allows access to video repo methods.
type VideoStore interface {
	AddVideo(v *models.Video, c *models.Channel) (videoID int64, err error)
	AddVideos(videos []*models.Video, c *models.Channel) ([]*models.Video, []error)
	GetDB() *sql.DB
	DeleteVideo(key, val string, chanID int64) error
	UpdateVideo(v *models.Video, c *models.Channel) error
}
