// Package contracts defines interfaces that decouple the application layer from storage implementations.
package contracts

import (
	"context"
	"database/sql"
	"errors"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
)

// ErrInvalidChannel wraps errors for channels rejected for their content (not storage failures), e.g. by AddChannel.
var ErrInvalidChannel = errors.New("invalid channel")

// Store allows access to the main store repo methods.
type Store interface {
	ChannelStore() ChannelStore
	DownloadStore() DownloadStore
	VideoStore() VideoStore
	TubarrSettingsStore() TubarrSettingsStore
}

// TubarrSettingsStore allows access to global settings and domain limit methods.
type TubarrSettingsStore interface {
	GetDB() *sql.DB
	GetSetting(key string) (value string, found bool, err error)
	SetSetting(key, value string) error
	GetDomainLimits() (map[string]models.DomainLimit, error)
	GetDomainLimitMap(key string) (map[string]int, error)
	SetDomainLimit(hostname string, limit models.DomainLimit) error
	DeleteDomainLimit(hostname string) error
}

// ChannelStore allows access to channel repo methods.
type ChannelStore interface {
	GetDB() *sql.DB
	WithTx(fn func(cs ChannelStore) error) error

	// Add operations.
	AddAuth(chanID int64, authDetails map[string]*models.ChannelAccessDetails) error
	AddChannel(c *models.Channel) (int64, error)
	AddChannelWithDetails(c *models.Channel, authDetails map[string]*models.ChannelAccessDetails, notifications []*models.Notification) error
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
	UpdateChannelURLsLastScan(ids []int64) error
	UpdateNewVideoURLs(key, val string, newVideoURLs []string) error
	SetNotificationDot(channelID int64, state bool) error
	AddNewVideoURLs(channelID int64, urls []string) error
	RemoveNewVideoURLs(channelID int64, urls []string) error

	// Delete operations.
	DeleteChannel(key, val string) error
	DeleteChannelURL(channelURLID int64) error
	DeleteVideosByURLs(channelID int64, urls []string, deleteFiles bool) error
	DeleteNotifyURLs(channelID int64, urls, names []string) error

	// 'Get' operations.
	GetAllChannels(mergeURLsWithParent bool) (channels []*models.Channel, hasRows bool, err error)
	GetDownloadedOrIgnoredVideos(c *models.Channel) (videos []*models.Video, hasRows bool, err error)
	GetDownloadedOrIgnoredVideoURLs(c *models.Channel) (urls, failedURLs []string, err error)
	GetAuth(channelID int64, url string) (username, password, loginURL string, err error)
	GetChannelID(key, val string) (int64, error)
	GetChannelName(key, val string) (string, error)
	GetChannelModel(key, val string, mergeURLsWithParent bool) (*models.Channel, bool, error)
	GetChannelURLModel(channelID int64, urlStr string, mergeWithParent bool) (chanURL *models.ChannelURL, hasRows bool, err error)
	GetChannelURLModels(c *models.Channel, mergeWithParent bool) ([]*models.ChannelURL, error)
	GetNotifyURLs(id int64) ([]*models.Notification, error)
	GetNewVideoURLs(key, val string) ([]string, error)

	// Other channel database functions.
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
	GetVideoChannelURLID(videoID int64) (channelURLID int64, err error)
	VideoExists(v *models.Video, channelID int64) (videoID int64, exists bool)
	GetVideosAwaitingMetarr(channelID int64, includeDone bool) (videos []*models.Video, err error)
	GetAvailableVideos(statuses ...consts.DownloadStatus) (videos []*models.AvailableVideo, err error)
	GetVideoOverrides(videoID int64) (*models.VideoOverrides, error)

	// Add operations.
	AddVideo(v *models.Video, channelID, channelURLID int64) (videoID int64, err error)
	AddVideos(videos []*models.Video, channelID int64) (videoModels []*models.Video, err error)
	AddAvailableVideos(videos []*models.Video, channelID int64) (addedURLs []string, err error)

	// Update operations.
	UpdateVideo(v *models.Video, channelID int64) error
	UpdateVideoPaths(videoID int64, oldVideo, oldJSON, newVideo, newJSON string) (updated bool, err error)
	ClaimVideos(channelID int64, videoIDs []int64, from []consts.DownloadStatus, status consts.DownloadStatus) (claimed []*models.Video, err error)
	SetVideoOverrides(videoIDs []int64, o *models.VideoOverrides) error
	SetQueuedVideoError(channelID int64, videoURL, errMsg string) error
	SetMissingVideoErrors(videoIDs []int64, errMsg string) error
}
