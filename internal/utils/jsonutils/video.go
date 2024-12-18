// Package jsonutils provides helper functions for JSON operations.
package jsonutils

import (
	"encoding/json"
	"fmt"

	"tubarr/internal/models"
)

// MarshalVideoJSON marshals all JSON elements for a video model.
func MarshalVideoJSON(v *models.Video) (metadata, settings, metarr []byte, err error) {
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
