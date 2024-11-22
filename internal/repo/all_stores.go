package repo

import (
	"database/sql"
	"tubarr/internal/models"
)

type Store struct {
	db           *sql.DB
	videoStore   *VideoStore
	channelStore *ChannelStore
}

// InitStores injects databases into the store methods.
func InitStores(db *sql.DB) *Store {
	return &Store{
		db:           db,
		videoStore:   GetVideoStore(db),
		channelStore: GetChannelStore(db),
	}
}

// GetChannelStore with pointer receiver.
func (s *Store) GetChannelStore() models.ChannelStore {
	return s.channelStore
}

// GetVideoStore with pointer receiver.
func (s *Store) GetVideoStore() models.VideoStore {
	return s.videoStore
}
