package models

import (
	"context"
	"database/sql"
)

// Store allows access to the main store repo methods.
type Store interface {
	ChannelStore() ChannelStore
	VideoStore() VideoStore
	DownloadStore() DownloadStore
}

// ChannelStore allows access to channel repo methods.
type ChannelStore interface {
	AddChannel(c *Channel) (int64, error)
	CrawlChannel(key, val string, s Store) error
	CrawlChannelIgnore(key, val string, s Store) error
	AddURLToIgnore(channelID int64, ignoreURL string) error
	DeleteChannel(key, val string) error
	FetchChannel(id int64) (c *Channel, err error, hasRows bool)
	FetchAllChannels() (channels []*Channel, err error, hasRows bool)
	LoadGrabbedURLs(c *Channel) (urls []string, err error)
	UpdateChannelRow(key, val, col, newVal string) error
	UpdateChannelEntry(chanKey, chanVal, updateKey, updateVal string) error
	UpdateLastScan(channelID int64) error
	UpdateChannelSettingsJSON(key, val string, updateFn func(*ChannelSettings) error) (int64, error)
	UpdateChannelMetarrArgsJSON(key, val string, updateFn func(*MetarrArgs) error) (int64, error)
	GetID(key, val string) (int64, error)
	AddNotifyURL(id int64, notifyName, notifyURL string) error
	GetNotifyURLs(id int64) ([]string, error)
	GetDB() *sql.DB
}

// VideoStore allows access to video repo methods.
type VideoStore interface {
	AddVideo(v *Video) (int64, error)
	AddVideos(videos []*Video, c *Channel) ([]*Video, []error)
	DeleteVideo(key, val string, chanID int64) error
	UpdateVideo(v *Video) error
	GetDB() *sql.DB
}

type DownloadStore interface {
	GetDB() *sql.DB
	UpdateDownloadStatuses(ctx context.Context, updates []StatusUpdate) error
	SetDownloadStatus(v *Video) error
}
