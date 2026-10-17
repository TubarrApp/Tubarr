// Package repo is used for performing database repository operations.
package repo

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// Store holds the database variable and sub-stores like ChannelStore etc.
type Store struct {
	db                  *sql.DB
	channelStore        *ChannelStore
	videoStore          *VideoStore
	downloadStore       *DownloadStore
	tubarrSettingsStore *TubarrSettingsStore
}

// InitStores injects databases into the store methods.
func InitStores(d *sql.DB) (*Store, error) {
	chanStore, err := GetChannelStore(d)
	if err != nil {
		return nil, err
	}

	return &Store{
		db:                  d,
		channelStore:        chanStore,
		videoStore:          GetVideoStore(d),
		downloadStore:       GetDownloadStore(d),
		tubarrSettingsStore: GetTubarrSettingsStore(d),
	}, nil
}

// ChannelStore with pointer receiver.
func (s *Store) ChannelStore() contracts.ChannelStore {
	return s.channelStore
}

// VideoStore with pointer receiver.
func (s *Store) VideoStore() contracts.VideoStore {
	return s.videoStore
}

// DownloadStore with pointer receiver.
func (s *Store) DownloadStore() contracts.DownloadStore {
	return s.downloadStore
}

// TubarrSettingsStore with pointer receiver.
func (s *Store) TubarrSettingsStore() contracts.TubarrSettingsStore {
	return s.tubarrSettingsStore
}

// ******************************** Private ***************************************************************************************

// marshalVideoMetadataJSON marshals all JSON elements for a channel URL/video model.
func marshalVideoMetadataJSON(v *models.Video) (metadata []byte, err error) {
	if v.MetadataMap != nil {
		metadata, err = json.Marshal(v.MetadataMap)
		if err != nil {
			return nil, fmt.Errorf("metadata marshal failed for video with URL %q: %w", v.URL, err)
		}
	}

	return metadata, nil
}

// makeSettingsMetarrArgsCopy makes a copy of the current Settings and MetarrArgs for comparison.
func makeSettingsMetarrArgsCopy(settings *models.Settings, metarrArgs *models.MetarrArgs, objectName string) (settingsCopy, metarrArgsCopy []byte) {
	var err error

	if settings != nil {
		settingsCopy, err = json.Marshal(settings)
		if err != nil {
			logger.Pl.E("Failed to make copy of Settings JSON for %q: %v", objectName, err)
		}
	}

	if metarrArgs != nil {
		metarrArgsCopy, err = json.Marshal(metarrArgs)
		if err != nil {
			logger.Pl.E("Failed to make copy of MetarrArgs JSON for %q: %v", objectName, err)
		}
	}

	return settingsCopy, metarrArgsCopy
}
