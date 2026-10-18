package app

import (
	"io"
	"path/filepath"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
)

// TestOverrideDir tests a Download As directory is filled in, a blank one keeps the channel URL's, and one whose
// fixed part doesn't exist (e.g. an unmounted drive) is refused.
func TestOverrideDir(t *testing.T) {
	logger.Pl.Console = io.Discard
	dir := t.TempDir()
	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{URL: "https://example.com/@cats"}
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	if got, err := overrideDir(dp, "", "/channel", "test"); err != nil || got != "/channel" {
		t.Errorf("blank: got %q, %v, want /channel", got, err)
	}
	if got, err := overrideDir(dp, filepath.Join(dir, "{{channel_name}}"), "/channel", "test"); err != nil || got != filepath.Join(dir, "Cats") {
		t.Errorf("templated: got %q, %v, want %q", got, err, filepath.Join(dir, "Cats"))
	}
	if _, err := overrideDir(dp, filepath.Join(dir, "unmounted", "videos"), "/channel", "test"); err == nil {
		t.Error("missing destination: got no error")
	}
}
