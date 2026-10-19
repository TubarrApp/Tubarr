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

// TestCheckManualVideos tests manual videos must be full http(s) URLs, with no channel URL or one of the channel's.
func TestCheckManualVideos(t *testing.T) {
	c := &models.Channel{Name: "Cats", URLModels: []*models.ChannelURL{{ID: 4, URL: "https://example.com/@cats"}}}
	for _, tc := range []struct {
		mv models.ManualVideo
		ok bool
	}{
		{models.ManualVideo{URL: "https://example.com/v/1"}, true},
		{models.ManualVideo{URL: "https://example.com/v/2", ChannelURLID: 4}, true},
		{models.ManualVideo{URL: "not a url"}, false},
		{models.ManualVideo{URL: "example.com/v/3"}, false},
		{models.ManualVideo{URL: "https://example.com/v/4", ChannelURLID: 5}, false},
		{models.ManualVideo{URL: "https://example.com/v/5", Auth: &models.ManualAuth{Username: "u", LoginURL: "https://example.com/login"}}, true},
		{models.ManualVideo{URL: "https://example.com/v/6", Auth: &models.ManualAuth{LoginURL: "https://example.com/login"}}, false},
		{models.ManualVideo{URL: "https://example.com/v/7", Auth: &models.ManualAuth{Username: "u", LoginURL: "example.com/login"}}, false},
	} {
		if bad := CheckManualVideos(c, []models.ManualVideo{tc.mv}); (len(bad) == 0) != tc.ok {
			t.Errorf("CheckManualVideos(%+v) = %v, want accepted: %t", tc.mv, bad, tc.ok)
		}
	}
	if cu, err := manualChannelURL(nil, c, 4); err != nil || cu != c.URLModels[0] {
		t.Errorf("manualChannelURL(4) = %v, %v; want the channel's URL", cu, err)
	}
	if _, err := manualChannelURL(nil, c, 5); err == nil {
		t.Error("manualChannelURL(5) found a URL the channel doesn't have")
	}
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
	urls := make([]string, 0, len(got))
	for _, cu := range got {
		urls = append(urls, cu.URL)
	}
	want := []string{"https://ok.example/@a", "https://templated.example/@d"}
	if !slices.Equal(urls, want) {
		t.Errorf("crawled URLs = %q, want %q", urls, want)
	}
}
