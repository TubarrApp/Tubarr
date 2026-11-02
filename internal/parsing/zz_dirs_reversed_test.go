package parsing

import (
	"io"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// TestParseDirectoryReversedDelimiters tests a path with "}}" before its "{{" is an error, not a panic that would end
// the program.
func TestParseDirectoryReversedDelimiters(t *testing.T) {
	logger.Pl.Console = io.Discard
	dp := NewDirectoryParser(&models.Channel{Name: "Cats"}, &models.ChannelURL{URL: "https://example.com/a"}, TubarrTags)

	if got, err := dp.ParseDirectory("/tmp/{{channel_name}}", "test"); err != nil || got != "/tmp/Cats" {
		t.Errorf("valid path = %q, %v, want /tmp/Cats", got, err)
	}
	for _, dir := range []string{"/tmp/}}x{{", "/tmp/{{channel_name}}}}{{", "}}{{"} {
		if got, err := dp.ParseDirectory(dir, "test"); err == nil {
			t.Errorf("%q = %q, want an error", dir, got)
		}
	}
}
