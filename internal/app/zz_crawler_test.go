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

// TestCheckManualURLs tests manual download lines must be full http(s) URLs, with piped ones naming one of the
// channel's URLs, matched ignoring case and a trailing slash.
func TestCheckManualURLs(t *testing.T) {
	c := &models.Channel{URLModels: []*models.ChannelURL{{URL: "https://example.com/@Cats/"}}}
	for line, ok := range map[string]bool{
		"https://example.com/v/1":                           true,
		"https://example.com/@cats|https://example.com/v/2": true,
		"not a url":       false,
		"example.com/v/3": false,
		"https://example.com/@dogs|https://example.com/v/4": false,
		"https://example.com/@cats|not a url":               false,
		"https://example.com/@cats|a|b":                     false,
	} {
		if bad := CheckManualURLs(c, []string{line}); (len(bad) == 0) != ok {
			t.Errorf("CheckManualURLs(%q) = %v, want accepted: %t", line, bad, ok)
		}
	}
	if cu, videoURL, err := parsePipedVideoURL("https://EXAMPLE.com/@cats|https://example.com/v/5", c); err != nil || cu != c.URLModels[0] || videoURL != "https://example.com/v/5" {
		t.Errorf("parsePipedVideoURL = %v, %q, %v; want the channel's URL", cu, videoURL, err)
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
	want := []string{"https://ok.example/@a", "https://metarr.example/@c", "https://templated.example/@d"} // Metarr creates its output dir.
	if !slices.Equal(urls, want) {
		t.Errorf("crawled URLs = %q, want %q", urls, want)
	}
}
