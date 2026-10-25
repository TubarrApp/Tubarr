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
	"tubarr/internal/validation"

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

// TestMakeMetarrCommandGPUNone tests "none" turns off an inherited GPU, passing Metarr no GPU or device node.
func TestMakeMetarrCommandGPUNone(t *testing.T) {
	logger.Pl.Console = io.Discard
	t.Cleanup(viper.Reset)
	c := &models.Channel{ID: 1, Name: "Cats"}
	v := &models.Video{URL: "https://example.com/v/1", VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}
	newURL := func(gpu string) *models.ChannelURL {
		return &models.ChannelURL{URL: "https://example.com/@a", ChanURLSettings: &models.Settings{},
			ChanURLMetarrArgs: &models.MetarrArgs{TranscodeGPU: gpu, TranscodeGPUNode: "/dev/dri/renderD128"}}
	}
	hasGPU := func(cu *models.ChannelURL) bool {
		args := makeMetarrCommand(v, cu, c, parsing.NewDirectoryParser(c, cu, parsing.AllTags))
		return slices.Contains(args, metkeys.TranscodeGPU) || slices.Contains(args, metkeys.TranscodeGPUNode)
	}

	if !hasGPU(newURL("vaapi")) {
		t.Error("the channel URL's GPU wasn't passed")
	}
	v.Overrides = &models.VideoOverrides{MetarrArgs: models.MetarrArgs{TranscodeGPU: validation.GPUNone}}
	if hasGPU(newURL("vaapi")) {
		t.Error("Download As 'none' still passed the channel URL's GPU")
	}
	v.Overrides = nil
	if hasGPU(newURL(validation.GPUNone)) {
		t.Error("a channel URL's 'none' was passed to Metarr")
	}
	if got, err := validation.ValidateGPUAcceleration("None"); err != nil || got != validation.GPUNone {
		t.Errorf("ValidateGPUAcceleration(None) = %q, %v; want it kept as %q", got, err, validation.GPUNone)
	}
}

// TestMakeMetarrCommandThumbnail tests a Download As thumbnail, or stripping it, is passed to Metarr.
func TestMakeMetarrCommandThumbnail(t *testing.T) {
	logger.Pl.Console = io.Discard
	t.Cleanup(viper.Reset)
	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{URL: "https://example.com/@a", ChanURLSettings: &models.Settings{}, ChanURLMetarrArgs: &models.MetarrArgs{}}
	v := &models.Video{URL: "https://example.com/v/1", VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}
	args := func() []string {
		return makeMetarrCommand(v, cu, c, parsing.NewDirectoryParser(c, cu, parsing.AllTags))
	}

	if a := args(); slices.Contains(a, metkeys.Thumbnail) || slices.Contains(a, metkeys.StripThumbnail) {
		t.Errorf("no Download As thumbnail, but got %q", a)
	}
	v.Overrides = &models.VideoOverrides{Thumbnail: "/home/tubarr/.tubarr/thumbnails/1.png"}
	if a := args(); !slices.Contains(a, "/home/tubarr/.tubarr/thumbnails/1.png") || slices.Index(a, metkeys.Thumbnail) != slices.Index(a, "/home/tubarr/.tubarr/thumbnails/1.png")-1 {
		t.Errorf("custom thumbnail not passed: %q", a)
	}
	v.Overrides = &models.VideoOverrides{StripThumbnail: true}
	if a := args(); !slices.Contains(a, metkeys.StripThumbnail) || slices.Contains(a, metkeys.Thumbnail) {
		t.Errorf("strip not passed alone: %q", a)
	}
}

// TestMakeMetarrCommandArgs tests the whole Metarr command for channel URL, global and Download As values.
func TestMakeMetarrCommandArgs(t *testing.T) {
	logger.Pl.Console = io.Discard
	t.Cleanup(viper.Reset)
	metaOps, err := parsing.ParseMetaOps([]string{"artist:set:Cat"})
	if err != nil {
		t.Fatal(err)
	}
	channelArgs := &models.MetarrArgs{
		OutputExt: "mp4", RenameStyle: "spaces", MinFreeMem: "1G", ExtraFFmpegArgs: "-x", OutputDir: "/out", MaxCPU: 50,
		MetaOps: metaOps, TranscodeGPU: "vaapi", TranscodeGPUNode: "/dev/dri/renderD128", TranscodeQuality: "23",
		TranscodeVideoCodecs: []string{"h264:hevc"}, TranscodeAudioCodecs: []string{"aac"},
	}
	globals := map[string]any{
		keys.MMinFreeMem: "2G", keys.OutputFiletype: "mkv", keys.MRenameStyle: "underscores", keys.MExtraFFmpegArgs: "-y",
		keys.DebugLevel: 2, keys.MMetaOps: []string{"title:set:Global"}, keys.MFilenameOps: []string{"prefix:[G] "},
		keys.MoveOnComplete: "/moved",
	}
	files := []string{metkeys.VideoFile, "/videos/v.mp4", metkeys.MetaFile, "/videos/v.json"}

	for _, tc := range []struct {
		name      string
		globals   map[string]any
		args      *models.MetarrArgs
		overrides *models.VideoOverrides
		want      []string
	}{
		{"channel URL", nil, channelArgs, nil, slices.Concat(files, []string{
			metkeys.FilenameOps, defaultFilenameSet,
			metkeys.MetaOps, "artist:set:Cat", metkeys.MetaOW,
			metkeys.MaxCPU, "50.00",
			metkeys.MinFreeMem, "1G",
			metkeys.OutputDir, "/out",
			metkeys.Ext, "mp4",
			metkeys.RenameStyle, "spaces",
			metkeys.ExtraFFmpegArgs, "-x",
			metkeys.TranscodeGPU, "vaapi",
			metkeys.TranscodeGPUNode, "/dev/dri/renderD128",
			metkeys.TranscodeVideoCodecs, "h264:hevc",
			metkeys.TranscodeQuality, "23",
			metkeys.TranscodeAudioCodecs, "aac",
		})},
		{"global", globals, &models.MetarrArgs{}, nil, slices.Concat(files, []string{
			metkeys.FilenameOps, "prefix:[G] ",
			metkeys.FilenameOps, defaultFilenameSet,
			metkeys.MetaOps, "title:set:Global", metkeys.MetaOW,
			metkeys.MinFreeMem, "2G",
			metkeys.OutputDir, "/moved",
			metkeys.Ext, "mkv",
			metkeys.RenameStyle, "underscores",
			metkeys.ExtraFFmpegArgs, "-y",
			metkeys.Debug, "2",
		})},
		{"Download As", globals, channelArgs, &models.VideoOverrides{
			MetarrArgs: models.MetarrArgs{OutputExt: "webm", TranscodeGPU: validation.GPUNone, MaxCPU: 80},
			Thumbnail:  "/thumb.png",
		}, slices.Concat(files, []string{
			metkeys.FilenameOps, defaultFilenameSet,
			metkeys.MaxCPU, "80.00",
			metkeys.MinFreeMem, "1G",
			metkeys.OutputDir, "/moved",
			metkeys.Ext, "webm",
			metkeys.RenameStyle, "spaces",
			metkeys.ExtraFFmpegArgs, "-x",
			metkeys.Thumbnail, "/thumb.png",
			metkeys.TranscodeVideoCodecs, "h264:hevc",
			metkeys.TranscodeQuality, "23",
			metkeys.TranscodeAudioCodecs, "aac",
			metkeys.Debug, "2",
		})},
		{"strip thumbnail", nil, &models.MetarrArgs{}, &models.VideoOverrides{StripThumbnail: true}, slices.Concat(files, []string{
			metkeys.FilenameOps, defaultFilenameSet,
			metkeys.StripThumbnail,
		})},
	} {
		viper.Reset()
		for k, val := range tc.globals {
			viper.Set(k, val)
		}
		c := &models.Channel{ID: 1, Name: "Cats"}
		cu := &models.ChannelURL{URL: "https://example.com/@a", ChanURLSettings: &models.Settings{}, ChanURLMetarrArgs: tc.args}
		v := &models.Video{URL: "https://example.com/v/1", VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json", Overrides: tc.overrides}
		if got := makeMetarrCommand(v, cu, c, parsing.NewDirectoryParser(c, cu, parsing.AllTags)); !slices.Equal(got, tc.want) {
			t.Errorf("%s:\ngot  %q\nwant %q", tc.name, got, tc.want)
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

	for _, tc := range []struct {
		name        string
		global, ops []string
		want        []string
	}{
		{"no ops", nil, nil, []string{defaultFilenameSet}},
		{"ops without a set", nil, []string{"prefix:[A] "}, []string{"prefix:[A] ", defaultFilenameSet}},
		{"own set", nil, []string{"set:{{title}}"}, []string{"set:{{title}}"}},
		{"global set", []string{"set:{{title}}"}, nil, []string{"set:{{title}}"}},
		{"global ops without a set", []string{"suffix: [G]"}, nil, []string{"suffix: [G]", defaultFilenameSet}},
		{"own ops replace global ones", []string{"set:{{title}}"}, []string{"prefix:[A] "}, []string{"prefix:[A] ", defaultFilenameSet}},
	} {
		viper.Reset()
		if tc.global != nil {
			viper.Set(keys.MFilenameOps, tc.global)
		}
		if got := withDefaultFilenameSet(&models.Video{}, tc.ops); !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}

	// Download As overrides replace the global ops, even when they have none.
	viper.Set(keys.MFilenameOps, []string{"set:{{title}}"})
	if got := withDefaultFilenameSet(&models.Video{Overrides: &models.VideoOverrides{}}, nil); !slices.Equal(got, []string{defaultFilenameSet}) {
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
