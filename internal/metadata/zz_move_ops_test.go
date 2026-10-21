package metadata

import (
	"io"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// TestHandleMoveOps tests move ops pick the first matching op's directory, by any filter type, for their channel URL.
func TestHandleMoveOps(t *testing.T) {
	logger.Pl.Console = io.Discard
	video := &models.Video{MetadataMap: map[string]any{"title": "Two Cats Playing", "duration": 4000}}

	tests := []struct {
		name string
		ops  []models.MetaFilterMoveOps
		want string
	}{
		{"saved before filter types, read as contains", []models.MetaFilterMoveOps{{Field: "title", Value: "cats", OutputDir: "/cats"}}, "/cats"},
		{"no match", []models.MetaFilterMoveOps{{Field: "title", FilterType: "contains", Value: "dogs", OutputDir: "/dogs"}}, ""},
		{"omits", []models.MetaFilterMoveOps{{Field: "title", FilterType: "omits", Value: "dogs", OutputDir: "/no-dogs"}}, "/no-dogs"},
		{"morethan", []models.MetaFilterMoveOps{{Field: "duration", FilterType: "morethan", Value: "3600", OutputDir: "/long"}}, "/long"},
		{"missing field for a comparison", []models.MetaFilterMoveOps{{Field: "views", FilterType: "morethan", Value: "1", OutputDir: "/popular"}}, ""},
		{"first match wins", []models.MetaFilterMoveOps{
			{Field: "title", FilterType: "contains", Value: "dogs", OutputDir: "/dogs"},
			{Field: "title", FilterType: "contains", Value: "two", OutputDir: "/two"},
			{Field: "title", FilterType: "contains", Value: "cats", OutputDir: "/cats"},
		}, "/two"},
		{"another channel URL's", []models.MetaFilterMoveOps{{ChannelURL: "https://tubesite.com/@other", Field: "title", FilterType: "contains", Value: "cats", OutputDir: "/cats"}}, ""},
	}
	for _, tt := range tests {
		cu := &models.ChannelURL{URL: "https://tubesite.com/@cats", ChanURLSettings: &models.Settings{MetaFilterMoveOps: tt.ops}}
		if got := HandleMoveOps(video, cu, nil); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}
