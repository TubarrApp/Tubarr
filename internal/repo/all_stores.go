package repo

import (
	"database/sql"
	"tubarr/internal/interfaces"
)

type Store struct {
	db           *sql.DB
	videoStore   *VideoStore
	channelStore *ChannelStore
}

func InitStores(db *sql.DB) *Store {
	return &Store{
		db:           db,
		videoStore:   GetVideoStore(db),
		channelStore: GetChannelStore(db),
	}
}

// GetChannelStore with pointer receiver
func (s *Store) GetChannelStore() interfaces.ChannelStore {
	return s.channelStore
}

// GetVideoStore with pointer receiver
func (s *Store) GetVideoStore() interfaces.VideoStore {
	return s.videoStore
}
