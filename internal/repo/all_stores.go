// Package repo is used for performing database operations.
package repo

import (
	"database/sql"
	"tubarr/internal/models"
)

type Store struct {
	db            *sql.DB
	videoStore    *VideoStore
	channelStore  *ChannelStore
	downloadStore *DownloadStore
}

// InitStores injects databases into the store methods.
func InitStores(db *sql.DB) *Store {
	return &Store{
		db:            db,
		videoStore:    GetVideoStore(db),
		channelStore:  GetChannelStore(db),
		downloadStore: GetDownloadStore(db),
	}
}

// ChannelStore with pointer receiver.
func (s *Store) ChannelStore() models.ChannelStore {
	return s.channelStore
}

// VideoStore with pointer receiver.
func (s *Store) VideoStore() models.VideoStore {
	return s.videoStore
}

// DownloadStore with pointer receiver.
func (s *Store) DownloadStore() models.DownloadStore {
	return s.downloadStore
}
