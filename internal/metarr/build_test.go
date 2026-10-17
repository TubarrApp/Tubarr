package metarr

import (
	"io"
	"testing"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/spf13/viper"
)

// TestParseMetarrOutputDirPerVideo tests --move-on-complete is filled afresh for each channel and URL, not the first one's.
func TestParseMetarrOutputDirPerVideo(t *testing.T) {
	logger.Pl.Console = io.Discard
	viper.Set(keys.MoveOnComplete, "/media/{{channel_name}}/{{channel_url}}")
	t.Cleanup(viper.Reset)

	for _, tc := range []struct{ channel, url, want string }{
		{"Cats", "https://example.com/@a", "/media/Cats/example.com-@a"},
		{"Cats", "https://example.com/@b", "/media/Cats/example.com-@b"},
		{"Dogs", "https://example.com/@c", "/media/Dogs/example.com-@c"},
	} {
		c := &models.Channel{ID: 1, Name: tc.channel}
		cu := &models.ChannelURL{URL: tc.url, ChanURLMetarrArgs: &models.MetarrArgs{}}
		if got := parseMetarrOutputDir(&models.Video{}, cu, parsing.NewDirectoryParser(c, cu, parsing.AllTags)); got != tc.want {
			t.Errorf("parseMetarrOutputDir(%s, %s) = %q, want %q", tc.channel, tc.url, got, tc.want)
		}
	}
}
