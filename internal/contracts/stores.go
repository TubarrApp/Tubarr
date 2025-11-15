// Package contracts defines interfaces that decouple the application layer from storage implementations.
package contracts

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
	GetDB() *sql.DB

	// Add operations.
	AddAuth(chanID int64, authDetails map[string]*models.ChannelAccessDetails) error
	AddChannel(c *models.Channel) (int64, error)
	AddChannelURL(channelID int64, cu *models.ChannelURL, isManual bool) (chanURLID int64, err error)
	AddNotifyURLs(channelID int64, notifications []*models.Notification) error
	AddURLToIgnore(channelID int64, ignoreURL string) error

	// Update operations.
	UpdateChannelFromConfig(c *models.Channel) (err error)
	UpdateChannelValue(key, val, col string, newVal any) error
	UpdateChannelURLSettings(cu *models.ChannelURL) error
	UpdateChannelMetarrArgsJSON(key, val string, updateFn func(*models.MetarrArgs) error) (int64, error)
	UpdateChannelSettingsJSON(key, val string, updateFn func(*models.Settings) error) (int64, error)
	UpdateLastScan(channelID int64) error

	// Delete operations.
	DeleteChannel(key, val string) error
	DeleteChannelURL(channelURLID int64) error
	DeleteVideosByURLs(channelID int64, urls []string) error
	DeleteNotifyURLs(channelID int64, urls, names []string) error

	// 'Get' operations.
	GetAllChannels(mergeURLsWithParent bool) (channels []*models.Channel, hasRows bool, err error)
	GetDownloadedOrIgnoredVideos(c *models.Channel) (videos []*models.Video, hasRows bool, err error)
	GetDownloadedOrIgnoredVideoURLs(c *models.Channel) (urls []string, err error)
	GetAuth(channelID int64, url string) (username, password, loginURL string, err error)
	GetChannelID(key, val string) (int64, error)
	GetChannelModel(key, val string, mergeURLsWithParent bool) (*models.Channel, bool, error)
	GetChannelURLModel(channelID int64, urlStr string, mergeWithParent bool) (chanURL *models.ChannelURL, hasRows bool, err error)
	GetChannelURLModels(c *models.Channel, mergeWithParent bool) ([]*models.ChannelURL, error)
	GetNotifyURLs(id int64) ([]*models.Notification, error)

	// Other channel database functions.
	CheckOrUnlockChannel(c *models.Channel) (bool, error)
	DisplaySettings(c *models.Channel)
}

// DownloadStore allows access to download repo methods.
type DownloadStore interface {
	GetDB() *sql.DB

	// Update operations.
	SetDownloadStatus(v *models.Video) error
	UpdateDownloadStatus(ctx context.Context, update models.StatusUpdate) error

	// Cancel operations.
	CancelDownload(videoID int64, videoURL string) bool
}

// VideoStore allows access to video repo methods.
type VideoStore interface {
	GetDB() *sql.DB

	// Get operations.
	GetVideoURLByID(videoID int64) (videoURL string, err error)

	// Add operations.
	AddVideo(v *models.Video, channelID, channelURLID int64) (videoID int64, err error)
	AddVideos(videos []*models.Video, channelID int64) (videoModels []*models.Video, err error)

	// Update operations.
	UpdateVideo(v *models.Video, channelID int64) error

	// Delete operations.
	DeleteVideo(videoURL string, channelID int64) error
}
