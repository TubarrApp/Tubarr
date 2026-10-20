package server

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestConfigFileResponse tests that the edit page is told which settings a channel's config file sets.
func TestConfigFileResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "channel.yaml")
	body := "channel-name: chan\ncrawl-freq: 30\nmetarr-output-ext: mkv\n" +
		"url-settings:\n  'https://example.com/@a':\n    settings:\n      auto-download: true\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := newConfigFileResponse(path)
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	if want := []string{"crawl-freq", "metarr-output-ext"}; !slices.Equal(got.Settings, want) {
		t.Errorf("settings %v, want %v", got.Settings, want)
	}
	if want := []string{"auto-download"}; !slices.Equal(got.URLSettings["https://example.com/@a"], want) {
		t.Errorf("URL settings %v, want %v for https://example.com/@a", got.URLSettings, want)
	}

	if got := newConfigFileResponse(filepath.Join(t.TempDir(), "missing.yaml")); got.Error == "" {
		t.Error("no error for a missing config file")
	}
}
