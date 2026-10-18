package metarr

import (
	"io"
	"slices"
	"testing"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/metkeys"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/spf13/viper"
)

// TestMakeMetarrCommandPurge tests a channel URL's metadata purge is passed to Metarr with its value, falling back to the
// global one.
func TestMakeMetarrCommandPurge(t *testing.T) {
	logger.Pl.Console = io.Discard
	t.Cleanup(viper.Reset)
	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{URL: "https://example.com/@a", ChanURLSettings: &models.Settings{}, ChanURLMetarrArgs: &models.MetarrArgs{MetaPurge: "json"}}
	v := &models.Video{URL: "https://example.com/v/1", VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}
	purge := func() string {
		t.Helper()
		args := makeMetarrCommand(v, cu, c, parsing.NewDirectoryParser(c, cu, parsing.AllTags))
		i := slices.Index(args, metkeys.PurgeMetaFile)
		if i < 0 || i+1 >= len(args) {
			return ""
		}
		return args[i+1]
	}

	if got := purge(); got != "json" {
		t.Errorf("channel URL purge passed as %q, want json", got)
	}
	cu.ChanURLMetarrArgs.MetaPurge = ""
	viper.Set(keys.MMetaPurge, "nfo")
	if got := purge(); got != "nfo" {
		t.Errorf("global purge passed as %q, want nfo", got)
	}
	viper.Reset()
	if got := purge(); got != "" {
		t.Errorf("purge passed as %q with none set", got)
	}
}

// TestWithDefaultFilenameSet tests finished videos are renamed to their full title unless an op that applies sets the
// name, and that channels without ops of their own still get the global ones.
func TestWithDefaultFilenameSet(t *testing.T) {
	logger.Pl.Console = io.Discard
	t.Cleanup(viper.Reset)
	if ops, err := parsing.ParseFilenameOps([]string{defaultFilenameSet}); err != nil || len(ops) != 1 {
		t.Fatalf("default op %q doesn't parse: %v", defaultFilenameSet, err)
	}

	cu := &models.ChannelURL{URL: "https://example.com/@a"}
	for _, tc := range []struct {
		name        string
		global, ops []string
		want        []string
	}{
		{"no ops", nil, nil, []string{defaultFilenameSet}},
		{"ops without a set", nil, []string{"prefix:[A] "}, []string{"prefix:[A] ", defaultFilenameSet}},
		{"own set", nil, []string{"set:{{title}}"}, []string{"set:{{title}}"}},
		{"another channel's set", nil, []string{"https://example.com/@b|set:{{title}}"}, []string{"https://example.com/@b|set:{{title}}", defaultFilenameSet}},
		{"global set", []string{"set:{{title}}"}, nil, []string{"set:{{title}}"}},
		{"global ops without a set", []string{"append: [G]"}, nil, []string{"append: [G]", defaultFilenameSet}},
		{"own ops replace global ones", []string{"set:{{title}}"}, []string{"prefix:[A] "}, []string{"prefix:[A] ", defaultFilenameSet}},
	} {
		viper.Reset()
		if tc.global != nil {
			viper.Set(keys.MFilenameOps, tc.global)
		}
		if got := withDefaultFilenameSet(cu, tc.ops); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

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
