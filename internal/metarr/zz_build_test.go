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

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/spf13/viper"
)

// TestMetaPurge tests the metadata purge isn't passed to Metarr, and is Download As's, else the channel URL's.
func TestMetaPurge(t *testing.T) {
	logger.Pl.Console = io.Discard
	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{URL: "https://example.com/@a", ChanURLSettings: &models.Settings{}, ChanURLMetarrArgs: &models.MetarrArgs{MetaPurge: "json"}}
	v := &models.Video{URL: "https://example.com/v/1", VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}
	if args := makeMetarrCommand(v, cu, c, parsing.NewDirectoryParser(c, cu, parsing.AllTags)); slices.Contains(args, metkeys.PurgeMetaFile) {
		t.Errorf("purge passed to Metarr: %q", args)
	}

	for _, tc := range []struct {
		name, override, url, want string
	}{
		{"Download As", "all", "json", "all"},
		{"channel URL", "", "JSON", "json"},
		{"none", "", "", ""},
	} {
		v.Overrides = &models.VideoOverrides{MetarrArgs: models.MetarrArgs{MetaPurge: tc.override}}
		cu.ChanURLMetarrArgs.MetaPurge = tc.url
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
	}{{"", false}, {"keep", false}, {"nfo", false}, {"json", true}, {"all", true}} {
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

// TestMakeMetarrCommandExtSame tests a channel URL's 'same' passes Metarr no output extension, so it keeps the video's.
func TestMakeMetarrCommandExtSame(t *testing.T) {
	logger.Pl.Console = io.Discard
	t.Cleanup(viper.Reset)
	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{URL: "https://example.com/@a", ChanURLSettings: &models.Settings{}, ChanURLMetarrArgs: &models.MetarrArgs{OutputExt: sharedconsts.ExtSame}}
	v := &models.Video{URL: "https://example.com/v/1", VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}
	if args := makeMetarrCommand(v, cu, c, parsing.NewDirectoryParser(c, cu, parsing.AllTags)); slices.Contains(args, metkeys.Ext) {
		t.Errorf("%q passed an extension to Metarr: %q", sharedconsts.ExtSame, args)
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

// TestMakeMetarrCommandArgs tests the whole Metarr command for channel URL and Download As values.
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
	globals := map[string]any{keys.DebugLevel: 2}
	files := []string{metkeys.VideoFile, "/videos/v.mp4", metkeys.MetaFile, "/videos/v.json"}
	no := false

	for _, tc := range []struct {
		name      string
		globals   map[string]any
		args      *models.MetarrArgs
		overrides *models.VideoOverrides
		want      []string
	}{
		{"channel URL", nil, channelArgs, nil, slices.Concat(files, []string{
			metkeys.MetaOps, "artist:set:Cat", metkeys.MetaOW,
			metkeys.MaxCPU, "50.00",
			metkeys.MinFreeMem, "1G",
			metkeys.OutputDir, "/out",
			metkeys.Ext, "mp4",
			metkeys.RenameStyle, "spaces",
			metkeys.PurgeOriginalVideo,
			metkeys.ExtraFFmpegArgs, "-x",
			metkeys.TranscodeGPU, "vaapi",
			metkeys.TranscodeGPUNode, "/dev/dri/renderD128",
			metkeys.TranscodeVideoCodecs, "h264:hevc",
			metkeys.TranscodeQuality, "23",
			metkeys.TranscodeAudioCodecs, "aac",
		})},
		{"Download As", globals, channelArgs, &models.VideoOverrides{
			MetarrArgs: models.MetarrArgs{OutputExt: "webm", TranscodeGPU: validation.GPUNone, MaxCPU: 80},
			Thumbnail:  "/thumb.png",
		}, slices.Concat(files, []string{
			metkeys.MaxCPU, "80.00",
			metkeys.MinFreeMem, "1G",
			metkeys.OutputDir, "/out",
			metkeys.Ext, "webm",
			metkeys.RenameStyle, "spaces",
			metkeys.PurgeOriginalVideo,
			metkeys.ExtraFFmpegArgs, "-x",
			metkeys.Thumbnail, "/thumb.png",
			metkeys.TranscodeVideoCodecs, "h264:hevc",
			metkeys.TranscodeQuality, "23",
			metkeys.TranscodeAudioCodecs, "aac",
			metkeys.Debug, "2",
		})},
		{"strip thumbnail", nil, &models.MetarrArgs{}, &models.VideoOverrides{StripThumbnail: true}, slices.Concat(files, []string{
			metkeys.PurgeOriginalVideo,
			metkeys.StripThumbnail,
		})},
		{"original kept", nil, &models.MetarrArgs{PurgeOriginalVideo: &no}, nil, files},
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

// TestMakeMetarrCommandChannelTags tests channel tags in ops are filled before Metarr gets them.
func TestMakeMetarrCommandChannelTags(t *testing.T) {
	logger.Pl.Console = io.Discard
	metaOps, err := parsing.ParseMetaOps([]string{"all-credits:set:{{channel_name}}", "title:prefix:{{channel_url_name}} - "})
	if err != nil {
		t.Fatal(err)
	}
	filenameOps, err := parsing.ParseFilenameOps([]string{"set:{{channel_name}} - {{fulltitle}}"})
	if err != nil {
		t.Fatal(err)
	}
	c := &models.Channel{ID: 1, Name: "Devon: Tracey"}
	cu := &models.ChannelURL{URL: "https://example.com/@a", ChanURLMetarrArgs: &models.MetarrArgs{MetaOps: metaOps, FilenameOps: filenameOps}}
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)
	want := []string{`all-credits:set:Devon\: Tracey`, "title:prefix:example.com/@a - ", `set:Devon\: Tracey - {{fulltitle}}`}

	for _, overrides := range []*models.VideoOverrides{nil, {MetaOps: metaOps, FilenameOps: filenameOps}} {
		v := &models.Video{URL: "https://example.com/v/1", VideoFilePath: "/v.mp4", JSONFilePath: "/v.json", Overrides: overrides}
		args := makeMetarrCommand(v, cu, c, dp)
		for _, op := range want {
			if !slices.Contains(args, op) {
				t.Errorf("Download As %v: args %q are missing %q", overrides != nil, args, op)
			}
		}
	}
}

// TestParseMetarrOutputDirPerVideo tests a templated output directory is filled afresh for each channel and URL, not the
// first one's.
func TestParseMetarrOutputDirPerVideo(t *testing.T) {
	logger.Pl.Console = io.Discard

	for _, tc := range []struct{ channel, url, want string }{
		{"Cats", "https://example.com/@a", "/media/Cats/example.com-@a"},
		{"Cats", "https://example.com/@b", "/media/Cats/example.com-@b"},
		{"Dogs", "https://example.com/@c", "/media/Dogs/example.com-@c"},
	} {
		c := &models.Channel{ID: 1, Name: tc.channel}
		cu := &models.ChannelURL{URL: tc.url, ChanURLMetarrArgs: &models.MetarrArgs{OutputDir: "/media/{{channel_name}}/{{channel_url}}"}}
		if got := parseMetarrOutputDir(&models.Video{}, cu, parsing.NewDirectoryParser(c, cu, parsing.AllTags)); got != tc.want {
			t.Errorf("parseMetarrOutputDir(%s, %s) = %q, want %q", tc.channel, tc.url, got, tc.want)
		}
	}
}
