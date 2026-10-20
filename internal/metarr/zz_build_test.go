package metarr

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/metkeys"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/spf13/viper"
)

// TestMetaPurge tests the metadata purge isn't passed to Metarr, and is Download As's, else the channel URL's, else the
// global one.
func TestMetaPurge(t *testing.T) {
	logger.Pl.Console = io.Discard
	t.Cleanup(viper.Reset)
	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{URL: "https://example.com/@a", ChanURLSettings: &models.Settings{}, ChanURLMetarrArgs: &models.MetarrArgs{MetaPurge: "json"}}
	v := &models.Video{URL: "https://example.com/v/1", VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}
	viper.Set(keys.MMetaPurge, "nfo")
	if args := makeMetarrCommand(v, cu, c, parsing.NewDirectoryParser(c, cu, parsing.AllTags)); slices.Contains(args, metkeys.PurgeMetaFile) {
		t.Errorf("purge passed to Metarr: %q", args)
	}

	for _, tc := range []struct {
		name, override, url, global, want string
	}{
		{"Download As", "all", "json", "nfo", "all"},
		{"channel URL", "", "json", "nfo", "json"},
		{"global", "", "", "NFO", "nfo"},
		{"none", "", "", "", ""},
	} {
		v.Overrides = &models.VideoOverrides{MetarrArgs: models.MetarrArgs{MetaPurge: tc.override}}
		cu.ChanURLMetarrArgs.MetaPurge = tc.url
		viper.Set(keys.MMetaPurge, tc.global)
		if got, err := metaPurge(v, cu); err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v, want %q", tc.name, got, err, tc.want)
		}
	}
}

// TestFinishMetafile tests the title, description and date are taken from the metadata file Metarr finished with, and
// the file is purged only when set to.
func TestFinishMetafile(t *testing.T) {
	logger.Pl.Console = io.Discard
	t.Cleanup(viper.Reset)
	for _, tc := range []struct {
		purge  string
		purged bool
	}{{"", false}, {"nfo", false}, {"json", true}, {"all", true}} {
		path := filepath.Join(t.TempDir(), "v.json")
		body := `{"title": "Old", "fulltitle": "[2024-01-02] New", "description": "Short", "longdescription": "Long", "upload_date": "20240102"}`
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cu := &models.ChannelURL{ChanURLMetarrArgs: &models.MetarrArgs{MetaPurge: tc.purge}}
		v := &models.Video{URL: "https://example.com/v/1", Title: "Stored", Description: "Stored", JSONFilePath: path}
		if err := finishMetafile(v, cu); err != nil {
			t.Fatal(err)
		}
		if v.Title != "[2024-01-02] New" || v.Description != "Long" || v.UploadDate.Format("2006-01-02") != "2024-01-02" {
			t.Errorf("purge %q: got title %q, description %q, date %v", tc.purge, v.Title, v.Description, v.UploadDate)
		}
		_, statErr := os.Stat(path)
		if purged := errors.Is(statErr, fs.ErrNotExist); purged != tc.purged || (v.JSONFilePath == "") != tc.purged {
			t.Errorf("purge %q: file purged %t, path stored %q, want purged %t", tc.purge, purged, v.JSONFilePath, tc.purged)
		}
	}
}

// TestMakeMetarrCommandOverrideArgs tests a video's Download As Metarr args replace its channel URL's, and blank ones
// use the channel URL's.
func TestMakeMetarrCommandOverrideArgs(t *testing.T) {
	logger.Pl.Console = io.Discard
	t.Cleanup(viper.Reset)
	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{URL: "https://example.com/@a", ChanURLSettings: &models.Settings{}, ChanURLMetarrArgs: &models.MetarrArgs{
		OutputExt: "mp4", RenameStyle: "spaces", TranscodeQuality: "23", TranscodeVideoCodecs: []string{"h264:hevc"}, MaxCPU: 50,
	}}
	v := &models.Video{URL: "https://example.com/v/1", VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}
	values := func(key string) []string {
		t.Helper()
		args := makeMetarrCommand(v, cu, c, parsing.NewDirectoryParser(c, cu, parsing.AllTags))
		var got []string
		for i := 0; i+1 < len(args); i++ {
			if args[i] == key {
				got = append(got, args[i+1])
			}
		}
		return got
	}

	for _, tc := range []struct {
		key        string
		plain, own []string
	}{
		{metkeys.Ext, []string{"mp4"}, []string{"mkv"}},
		{metkeys.TranscodeVideoCodecs, []string{"h264:hevc"}, []string{"av1"}},
		{metkeys.MaxCPU, []string{"50.00"}, []string{"80.00"}},
		{metkeys.RenameStyle, []string{"spaces"}, []string{"spaces"}},
		{metkeys.TranscodeQuality, []string{"23"}, []string{"23"}},
	} {
		v.Overrides = nil
		if got := values(tc.key); !slices.Equal(got, tc.plain) {
			t.Errorf("%s without overrides: got %q, want %q", tc.key, got, tc.plain)
		}
		v.Overrides = &models.VideoOverrides{MetarrArgs: models.MetarrArgs{OutputExt: "mkv", TranscodeVideoCodecs: []string{"av1"}, MaxCPU: 80}}
		if got := values(tc.key); !slices.Equal(got, tc.own) {
			t.Errorf("%s with overrides: got %q, want %q", tc.key, got, tc.own)
		}
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
		if got := withDefaultFilenameSet(&models.Video{}, cu, tc.ops); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}

	// Download As overrides replace the global ops, even when they have none.
	viper.Set(keys.MFilenameOps, []string{"set:{{title}}"})
	if got := withDefaultFilenameSet(&models.Video{Overrides: &models.VideoOverrides{}}, cu, nil); !slices.Equal(got, []string{defaultFilenameSet}) {
		t.Errorf("overridden: got %q, want only %q", got, defaultFilenameSet)
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
