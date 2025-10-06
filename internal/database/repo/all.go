// Package repo is used for performing database operations.
package repo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"tubarr/internal/interfaces"
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
func (s *Store) ChannelStore() interfaces.ChannelStore {
	return s.channelStore
}

// VideoStore with pointer receiver.
func (s *Store) VideoStore() interfaces.VideoStore {
	return s.videoStore
}

// DownloadStore with pointer receiver.
func (s *Store) DownloadStore() interfaces.DownloadStore {
	return s.downloadStore
}

// ******************************** Private ********************************

// marshalVideoJSON marshals all JSON elements for a video model.
func marshalVideoJSON(v *models.Video) (metadata, settings, metarr []byte, err error) {
	if v.MetadataMap != nil {
		metadata, err = json.Marshal(v.MetadataMap)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("metadata marshal failed for video with URL %q: %w", v.URL, err)
		}
	}

	settings, err = json.Marshal(v.Settings)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("settings marshal failed for video with URL %q: %w", v.URL, err)
	}

	metarr, err = json.Marshal(v.MetarrArgs)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("metarr marshal failed for video with URL %q: %w", v.URL, err)
	}

	return metadata, settings, metarr, nil
}
