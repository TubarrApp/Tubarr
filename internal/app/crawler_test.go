package app

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// TestMain sets up the test environment for the app package.
func TestMain(m *testing.M) {
	logger.Pl.Console = io.Discard
	os.Exit(m.Run())
}

// TestFilterSkipsUnavailableDestinations tests that a channel URL is only crawled when its destinations are available.
func TestFilterSkipsUnavailableDestinations(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "unmounted", "videos")
	c := &models.Channel{
		Name:           "Test",
		ChanSettings:   &models.Settings{VideoDir: dir},
		ChanMetarrArgs: &models.MetarrArgs{},
		URLModels: []*models.ChannelURL{
			{URL: "https://ok.example/@a"},
			{URL: "https://missing.example/@b", ChanURLSettings: &models.Settings{VideoDir: missing}},
			{URL: "https://metarr.example/@c", ChanURLMetarrArgs: &models.MetarrArgs{OutputDir: missing}},
			{URL: "https://templated.example/@d", ChanURLSettings: &models.Settings{VideoDir: filepath.Join(dir, "{{channel_name}}")}},
		},
	}

	got, _ := filterAllowedValidURLs(c)
	var urls []string
	for _, cu := range got {
		urls = append(urls, cu.URL)
	}
	want := []string{"https://ok.example/@a", "https://metarr.example/@c", "https://templated.example/@d"} // Metarr creates its output dir.
	if !slices.Equal(urls, want) {
		t.Errorf("crawled URLs = %q, want %q", urls, want)
	}
}
