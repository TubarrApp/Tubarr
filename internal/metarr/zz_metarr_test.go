package metarr

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/metkeys"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/repo"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/logging"
	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedparsing"
	"github.com/spf13/viper"
)

// ******************************** Build *********************************************************************************************

// TestMetaPurge tests the metadata purge isn't passed to Metarr, and is Download As's, else the channel URL's.
func TestMetaPurge(t *testing.T) {
	logger.Pl.Console = io.Discard
	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{URL: "https://example.com/@a", ChanURLSettings: &models.Settings{}, ChanURLMetarrArgs: &models.MetarrArgs{MetaPurge: "json"}}
	v := &models.Video{URL: "https://example.com/v/1", Files: models.VideoFiles{VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}}
	if args := makeMetarrCommand(v, cu, c, parsing.NewDirectoryParser(c, cu, parsing.AllTags)); slices.Contains(args, "--purge-metafile") {
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
		v := &models.Video{URL: "https://example.com/v/1", Metadata: models.VideoMetadata{Title: "Stored", Description: "Stored"}, Files: models.VideoFiles{JSONFilePath: path}}
		if err := finishMetafile(v, cu); err != nil {
			t.Fatal(err)
		}
		if v.Metadata.Title != "[2024-01-02] New" || v.Metadata.Description != "Long" || v.Metadata.UploadDate.Format("2006-01-02") != "2024-01-02" {
			t.Errorf("purge %q: got title %q, description %q, date %v", tc.purge, v.Metadata.Title, v.Metadata.Description, v.Metadata.UploadDate)
		}
		_, statErr := os.Stat(path)
		if purged := errors.Is(statErr, fs.ErrNotExist); purged != tc.purged || (v.Files.JSONFilePath == "") != tc.purged {
			t.Errorf("purge %q: file purged %t, path stored %q, want purged %t", tc.purge, purged, v.Files.JSONFilePath, tc.purged)
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
		OutputExt: "mp4", RenameStyle: "spaces", TranscodeVideoArgs: []string{"hevc:-crf 23"}, TranscodeVideoCodecs: []string{"h264:hevc"}, MaxCPU: 50,
	}}
	v := &models.Video{URL: "https://example.com/v/1", Files: models.VideoFiles{VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}}
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
		{metkeys.TranscodeVideoArgs, []string{"hevc:-crf 23"}, []string{"av1:-crf 35"}},
	} {
		v.Overrides = nil
		if got := values(tc.key); !slices.Equal(got, tc.plain) {
			t.Errorf("%s without overrides: got %q, want %q", tc.key, got, tc.plain)
		}
		v.Overrides = &models.VideoOverrides{MetarrArgs: models.MetarrArgs{
			OutputExt: "mkv", TranscodeVideoCodecs: []string{"av1"}, TranscodeVideoArgs: []string{"av1:-crf 35"}, MaxCPU: 80,
		}}
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
	v := &models.Video{URL: "https://example.com/v/1", Files: models.VideoFiles{VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}}
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
	v := &models.Video{URL: "https://example.com/v/1", Files: models.VideoFiles{VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}}
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
	v := &models.Video{URL: "https://example.com/v/1", Files: models.VideoFiles{VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}}
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

// TestMakeMetarrCommandArgs tests the whole Metarr command for channel URL and Download As values, with Tubarr's debug
// level.
func TestMakeMetarrCommandArgs(t *testing.T) {
	logger.Pl.Console = io.Discard
	prevLevel := logging.Level()
	t.Cleanup(func() { logging.SetLevel(prevLevel) })
	metaOps, err := parsing.ParseMetaOps([]string{"artist:set:Cat"})
	if err != nil {
		t.Fatal(err)
	}
	channelArgs := &models.MetarrArgs{
		OutputExt: "mp4", RenameStyle: "spaces", MinFreeMem: "1G", ExtraFFmpegArgs: "-x", OutputDir: "/out", MaxCPU: 50,
		MetaOps: metaOps, TranscodeGPU: "vaapi", TranscodeGPUNode: "/dev/dri/renderD128", TranscodeVideoArgs: []string{"hevc:-crf 23"},
		TranscodeVideoCodecs: []string{"h264:hevc"}, TranscodeAudioCodecs: []string{"aac"},
	}
	files := []string{metkeys.VideoFile, "/videos/v.mp4", metkeys.MetaFile, "/videos/v.json"}
	no := false
	yes := true
	nfoFiles := slices.Concat(files, []string{metkeys.WriteNFO})

	for _, tc := range []struct {
		name      string
		level     int // Tubarr's debug level, which Metarr is given when above 0.
		args      *models.MetarrArgs
		overrides *models.VideoOverrides
		want      []string
	}{
		{"channel URL", 0, channelArgs, nil, slices.Concat(files, []string{
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
			metkeys.TranscodeVideoArgs, "hevc:-crf 23",
			metkeys.TranscodeAudioCodecs, "aac",
		})},
		{"Download As", 2, channelArgs, &models.VideoOverrides{
			MetarrArgs: models.MetarrArgs{
				OutputExt: "webm", TranscodeGPU: validation.GPUNone, MaxCPU: 80, ExtraFFmpegArgs: "-x",
				TranscodeVideoCodecs: []string{"h264:hevc"}, TranscodeVideoArgs: []string{"hevc:-crf 23"}, TranscodeAudioCodecs: []string{"aac"},
			},
			Thumbnail: "/thumb.png",
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
			metkeys.TranscodeVideoArgs, "hevc:-crf 23",
			metkeys.TranscodeAudioCodecs, "aac",
			metkeys.Debug, "2",
		})},
		{"transcode boxes emptied in Download As", 0, channelArgs, &models.VideoOverrides{}, slices.Concat(files, []string{
			metkeys.MaxCPU, "50.00",
			metkeys.MinFreeMem, "1G",
			metkeys.OutputDir, "/out",
			metkeys.Ext, "mp4",
			metkeys.RenameStyle, "spaces",
			metkeys.PurgeOriginalVideo,
			metkeys.TranscodeGPU, "vaapi",
			metkeys.TranscodeGPUNode, "/dev/dri/renderD128",
		})},
		{"one transcode box edited in Download As", 0, &models.MetarrArgs{PurgeOriginalVideo: &no, TranscodeVideoCodecs: []string{"h264:hevc"}, TranscodeVideoArgs: []string{"hevc:-crf 23"}},
			&models.VideoOverrides{MetarrArgs: models.MetarrArgs{TranscodeVideoArgs: []string{"hevc:-crf 30"}}},
			slices.Concat(files, []string{metkeys.TranscodeVideoArgs, "hevc:-crf 30"})},
		{"extension and rename style turned off in Download As", 0, &models.MetarrArgs{OutputExt: "mp4", RenameStyle: "spaces", PurgeOriginalVideo: &no},
			&models.VideoOverrides{MetarrArgs: models.MetarrArgs{OutputExt: "same", RenameStyle: "skip"}},
			slices.Concat(files, []string{metkeys.RenameStyle, "skip"})},
		{"strip thumbnail", 0, &models.MetarrArgs{}, &models.VideoOverrides{StripThumbnail: true}, slices.Concat(files, []string{
			metkeys.PurgeOriginalVideo,
			metkeys.StripThumbnail,
		})},
		{"original kept", 0, &models.MetarrArgs{PurgeOriginalVideo: &no}, nil, files},
		{"original kept in Download As", 0, &models.MetarrArgs{}, &models.VideoOverrides{MetarrArgs: models.MetarrArgs{PurgeOriginalVideo: &no}}, files},
		{"original kept, unset in Download As", 0, &models.MetarrArgs{PurgeOriginalVideo: &no}, &models.VideoOverrides{}, files},
		{"original kept, deleted in Download As", 0, &models.MetarrArgs{PurgeOriginalVideo: &no}, &models.VideoOverrides{MetarrArgs: models.MetarrArgs{PurgeOriginalVideo: &yes}},
			slices.Concat(files, []string{metkeys.PurgeOriginalVideo})},
		{"NFO unset", 0, &models.MetarrArgs{PurgeOriginalVideo: &no}, nil, files},
		{"NFO yes", 0, &models.MetarrArgs{PurgeOriginalVideo: &no, WriteNFO: &yes}, nil, nfoFiles},
		{"NFO no", 0, &models.MetarrArgs{PurgeOriginalVideo: &no, WriteNFO: &no}, nil, files},
		{"NFO yes, unset in Download As", 0, &models.MetarrArgs{PurgeOriginalVideo: &no, WriteNFO: &yes}, &models.VideoOverrides{}, nfoFiles},
		{"NFO yes, no in Download As", 0, &models.MetarrArgs{PurgeOriginalVideo: &no, WriteNFO: &yes}, &models.VideoOverrides{MetarrArgs: models.MetarrArgs{WriteNFO: &no}}, files},
		{"NFO no, yes in Download As", 0, &models.MetarrArgs{PurgeOriginalVideo: &no, WriteNFO: &no}, &models.VideoOverrides{MetarrArgs: models.MetarrArgs{WriteNFO: &yes}}, nfoFiles},
		{"NFO unset, yes in Download As", 0, &models.MetarrArgs{PurgeOriginalVideo: &no}, &models.VideoOverrides{MetarrArgs: models.MetarrArgs{WriteNFO: &yes}}, nfoFiles},
	} {
		logging.SetLevel(tc.level)
		c := &models.Channel{ID: 1, Name: "Cats"}
		cu := withOwnLevel(&models.ChannelURL{URL: "https://example.com/@a", ChanURLSettings: &models.Settings{}, ChanURLMetarrArgs: tc.args})
		v := &models.Video{URL: "https://example.com/v/1", Files: models.VideoFiles{VideoFilePath: "/videos/v.mp4", JSONFilePath: "/videos/v.json"}, Overrides: tc.overrides}
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
	cu := withOwnLevel(&models.ChannelURL{URL: "https://example.com/@a", ChanURLMetarrArgs: &models.MetarrArgs{MetaOps: metaOps, FilenameOps: filenameOps}})
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)
	want := []string{`all-credits:set:Devon\: Tracey`, "title:prefix:example.com/@a - ", `set:Devon\: Tracey - {{fulltitle}}`}

	for _, overrides := range []*models.VideoOverrides{nil, {MetaOps: metaOps, FilenameOps: filenameOps}} {
		v := &models.Video{URL: "https://example.com/v/1", Files: models.VideoFiles{VideoFilePath: "/v.mp4", JSONFilePath: "/v.json"}, Overrides: overrides}
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

// ******************************** Check *********************************************************************************************

// TestSkipReason tests each reason Metarr is held back for a channel URL, including for a file it inherits, and that it
// isn't otherwise.
func TestSkipReason(t *testing.T) {
	logger.Pl.Console = io.Discard
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.txt")
	c := &models.Channel{ID: 1, Name: "Cats"}

	for name, tc := range map[string]struct {
		installErr error
		args       models.MetarrArgs
		want       models.SkipMetarrReasons
	}{
		"runs":                       {},
		"no metarr":                  {installErr: errors.New("not found"), want: models.SkipMetarrNoPath},
		"meta ops file":              {args: models.MetarrArgs{MetaOpsFile: missing}, want: models.SkipMetarrMissingMetaOpsFile},
		"filename ops file":          {args: models.MetarrArgs{FilenameOpsFile: missing}, want: models.SkipMetarrMissingFilenameOpsFile},
		"filtered meta ops file":     {args: models.MetarrArgs{FilteredMetaOpsFile: missing}, want: models.SkipMetarrMissingFilteredMetaOpsFile},
		"filtered filename ops file": {args: models.MetarrArgs{FilteredFilenameOpsFile: missing}, want: models.SkipMetarrMissingFilteredFilenameOpsFile},
	} {
		cu := &models.ChannelURL{URL: "https://example.com/@cats", ChanURLMetarrArgs: &models.MetarrArgs{}}
		cu.Levels = []models.SettingsLevel{{MetarrArgs: &tc.args}, {MetarrArgs: cu.ChanURLMetarrArgs}} // Inherited from its channel.
		skip, err := SkipReason(tc.installErr, cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
		if skip.Skip != (tc.want != "") || skip.Reason != tc.want || (err != nil) != skip.Skip {
			t.Errorf("%s: SkipReason = %+v, %v; want reason %q", name, skip, err, tc.want)
		}
	}
}

// ******************************** Follow ops ****************************************************************************************

// TestFollowChannelURLOps tests the channel URL's lines in saved Download As ops are refreshed from its ops and ops
// files as they are now, keeping the lines added or deleted for the video.
func TestFollowChannelURLOps(t *testing.T) {
	logger.Pl.Console = io.Discard
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	filtered := func(lines ...string) []models.FilteredMetaOps {
		t.Helper()
		ops, err := parsing.ParseFilteredMetaOps(lines)
		if err != nil {
			t.Fatal(err)
		}
		return ops
	}

	metaOpsFile := write("meta.txt", "album:set:Old\ngenre:set:Pets\n")
	c := &models.Channel{ID: 1, Name: "Test"}
	cu := withOwnLevel(&models.ChannelURL{URL: ownURL, ChanURLSettings: &models.Settings{}, ChanURLMetarrArgs: &models.MetarrArgs{
		MetaOpsFile:     metaOpsFile,
		FilenameOpsFile: write("filename.txt", "suffix: [Old]\n"),
		FilteredMetaOps: filtered("title:contains:cat||genre:set:Cats"),
	}})
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	// Saved with a meta op deleted, the filtered meta box left as the channel URL's, and a filename op added.
	was, err := ChannelURLOpLines(cu, dp)
	if err != nil {
		t.Fatal(err)
	}
	if was.MetaOps != "album:set:Old\ngenre:set:Pets" {
		t.Fatalf("channel URL's meta ops %q, want the file's", was.MetaOps)
	}
	o := &models.VideoOverrides{ChannelURLOps: was}
	if o.MetaOps, err = parsing.ParseMetaOps([]string{"album:set:Old"}); err != nil {
		t.Fatal(err)
	}
	o.FilteredMetaOps = filtered(opLines(was.FilteredMetaOps)...)
	if o.FilenameOps, err = parsing.ParseFilenameOps(append(opLines(was.FilenameOps), "prefix:[Mine] ")); err != nil {
		t.Fatal(err)
	}

	// The channel URL's ops files and typed ops change.
	write("meta.txt", "album:set:New\ngenre:set:Pets\n")
	write("filename.txt", "suffix: [New]\n")
	cu.ChanURLMetarrArgs.FilteredMetaOps = filtered("title:contains:dog||genre:set:Dogs")
	now, err := ChannelURLOpLines(cu, dp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(now.MetaOps, "album:set:New") || strings.Contains(now.MetaOps, "Old") {
		t.Fatalf("channel URL's meta ops now %q, want the file's new op", now.MetaOps)
	}

	// Following twice gives the same ops.
	for range 2 {
		if err := FollowChannelURLOps(o, cu, dp); err != nil {
			t.Fatal(err)
		}
		got := VideoOverrideLines(o)
		if want := "album:set:New"; got.MetaOps != want {
			t.Errorf("box with a deleted op: got %q, want %q, the file's new op in and the deleted one still out", got.MetaOps, want)
		}
		if got.FilteredMetaOps != now.FilteredMetaOps {
			t.Errorf("box left as the channel URL's: got %q, want its ops now, %q", got.FilteredMetaOps, now.FilteredMetaOps)
		}
		if want := "prefix:[Mine] \nsuffix: [New]"; got.FilenameOps != want {
			t.Errorf("box with an added op: got %q, want %q, the added op kept and the file's suffix swapped", got.FilenameOps, want)
		}
		if *o.ChannelURLOps != *now {
			t.Errorf("channel URL's ops kept as %+v, want %+v", *o.ChannelURLOps, *now)
		}
	}

	// Overrides without the channel URL's ops stay as saved.
	old, err := parsing.ParseMetaOps([]string{"album:set:Old"})
	if err != nil {
		t.Fatal(err)
	}
	kept := &models.VideoOverrides{MetarrArgs: models.MetarrArgs{MetaOps: old}}
	if err := FollowChannelURLOps(kept, cu, dp); err != nil || VideoOverrideLines(kept).MetaOps != "album:set:Old" {
		t.Errorf("overrides without the channel URL's ops: got %q, %v, want them as saved", VideoOverrideLines(kept).MetaOps, err)
	}

	// An ops file that can't be read is an error.
	if err := os.Remove(metaOpsFile); err != nil {
		t.Fatal(err)
	}
	if err := FollowChannelURLOps(o, cu, dp); err == nil {
		t.Error("expected an error for a missing ops file")
	}
}

// ******************************** Levels ********************************************************************************************

// TestOpsFilesFromEveryLevel tests a channel URL loaded from the database gets the ops of every level's file, a
// profile's file losing to its channel's typed op where both set the same thing.
func TestOpsFilesFromEveryLevel(t *testing.T) {
	logger.Pl.Console = io.Discard
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	cs, err := repo.GetChannelStore(db)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	parse := func(ops ...string) []models.MetaOps {
		t.Helper()
		parsed, err := parsing.ParseMetaOps(ops)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}

	profile := &models.Profile{Name: "files", ProfileSettings: &models.Settings{}, ProfileMetarrArgs: &models.MetarrArgs{
		MetaOps:     parse("title:prefix:[P] "),
		MetaOpsFile: write("profile.txt", "title:set:From Profile File\nartist:set:Profile File\n"),
	}}
	if err := cs.AddProfile(profile); err != nil {
		t.Fatal(err)
	}
	c := &models.Channel{
		Name:           "Files",
		ProfileID:      profile.ID,
		ChanSettings:   &models.Settings{VideoDir: t.TempDir()},
		ChanMetarrArgs: &models.MetarrArgs{MetaOps: parse("title:set:From Channel")},
		URLModels: []*models.ChannelURL{{
			URL:               ownURL,
			ChanURLMetarrArgs: &models.MetarrArgs{MetaOpsFile: write("url.txt", "album:set:URL File\n")},
		}},
	}
	if err := cs.AddChannelWithDetails(c, nil, nil); err != nil {
		t.Fatal(err)
	}

	loaded, _, err := cs.GetChannelModel(consts.QChanID, strconv.FormatInt(c.ID, 10), true)
	if err != nil || loaded.LoadErr != nil {
		t.Fatalf("GetChannelModel = %v, LoadErr %v", err, loaded.LoadErr)
	}
	cu := loaded.URLModels[0]
	ops, err := channelURLMetaOps(cu, parsing.NewDirectoryParser(loaded, cu, parsing.TubarrTags))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"artist:set:Profile File", "title:prefix:[P] ", "title:set:From Channel", "album:set:URL File"}
	if got := getDedupedMetaOpStrings(ops); !slices.Equal(got, want) {
		t.Errorf("meta ops = %q, want %q", got, want)
	}
}

// ******************************** Output ********************************************************************************************

// TestParseProgress tests Metarr's progress lines are read, with -1 for an unknown percentage, and others ignored.
func TestParseProgress(t *testing.T) {
	tests := []struct {
		line string
		want Progress
		ok   bool
	}{
		{"metarr progress: pct=25.0 frame=120 fps=29.9 speed=1.52x eta=95", Progress{Percent: 25, Frame: 120, FPS: 29.9, Speed: "1.52x", ETA: 95}, true},
		{"metarr progress: pct=-1.0 frame=200 fps=30.0 speed= eta=-1", Progress{Percent: -1, Frame: 200, FPS: 30, ETA: -1}, true},
		{"metarr progress: pct=10.0 frame=5 fps=1.0 speed=1x", Progress{Percent: 10, Frame: 5, FPS: 1, Speed: "1x", ETA: -1}, true}, // An older Metarr.
		{"metarr progress: pct=40.0 frame=50 fps=25.0 speed=2x eta=12 pass=2 passes=3",
			Progress{Percent: 40, Frame: 50, FPS: 25, Speed: "2x", ETA: 12, Pass: 2, Passes: 3}, true},
		{"final video path: /videos/a.mp4", Progress{}, false},
	}
	for _, tt := range tests {
		got, ok := parseProgress(tt.line)
		if ok != tt.ok || got != tt.want {
			t.Errorf("parseProgress(%q) = %+v, %t; want %+v, %t", tt.line, got, ok, tt.want, tt.ok)
		}
	}
}

// TestRunWithProgress tests progress lines are recorded while Metarr runs and cleared after, leaving the rest of stdout.
func TestRunWithProgress(t *testing.T) {
	const videoID = 42
	script := `echo "metarr progress: pct=50.0 frame=10 fps=5.0 speed=1x"
read _
echo "final video path: /videos/a.mp4"`
	cmd := exec.Command("sh", "-c", script)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	var out string
	var runErr error
	go func() {
		out, runErr = runWithProgress(cmd, videoID)
		close(done)
	}()

	// Metarr waits on stdin after its progress line, so the progress is visible until it's let go.
	for VideoProgress(videoID) == nil {
		select {
		case <-done:
			t.Fatal("finished before its progress was recorded")
		default:
		}
	}
	if p := VideoProgress(videoID); p.Percent != 50 || p.Frame != 10 {
		t.Errorf("progress = %+v", p)
	}
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	<-done

	if runErr != nil {
		t.Fatal(runErr)
	}
	if strings.TrimSpace(out) != "final video path: /videos/a.mp4" {
		t.Errorf("stdout = %q, want only the path line", out)
	}
	if p := VideoProgress(videoID); p != nil {
		t.Errorf("progress %+v left after the run", p)
	}
}

// TestMetarrErrors tests that Metarr's reported errors are read from its output once each, ignoring other lines.
func TestMetarrErrors(t *testing.T) {
	stdout := "final video path: /a.mp4\n" +
		"metarr error: video codec \"h264\" can't go in a webm file\n" +
		"metarr error: \n" +
		"some log line mentioning metarr error: inside\n" +
		"metarr error: video codec \"h264\" can't go in a webm file\n" +
		"metarr error: disk full"
	want := []string{`video codec "h264" can't go in a webm file`, "disk full"}
	if got := metarrErrors(stdout); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := metarrErrors("final video path: /a.mp4\n"); got != nil {
		t.Errorf("got %q from output with no errors, want none", got)
	}
}

// ******************************** Overrides *****************************************************************************************

const (
	ownURL   = "https://example.com/@a"
	otherURL = "https://example.com/@b"
)

// withOwnLevel gives a channel URL built in a test its own settings as its only level, as loading it would.
func withOwnLevel(cu *models.ChannelURL) *models.ChannelURL {
	cu.Levels = []models.SettingsLevel{{Settings: cu.ChanURLSettings, MetarrArgs: cu.ChanURLMetarrArgs}}
	return cu
}

// TestChannelURLOverrideLines tests the Download As dialog gets the channel URL's directories and ops, and that its
// lines parse back.
func TestChannelURLOverrideLines(t *testing.T) {
	logger.Pl.Console = io.Discard

	metaOps, _, err := sharedparsing.ParseMetaOps([]string{"title:prefix:[A] ", "artist:set:Cats"})
	if err != nil {
		t.Fatal(err)
	}
	filteredMetaOps, _, err := sharedparsing.ParseFilteredMetaOps([]string{
		"title:contains:cat||genre:set:Cats",
		"title:contains:cat&&duration:more-than:600||album:set:Long Cats",
	})
	if err != nil {
		t.Fatal(err)
	}
	filteredFilenameOps, _, err := sharedparsing.ParseFilteredFilenameOps([]string{"title:contains:cat&&duration:more-than:600||prefix:[LONG CAT] "})
	if err != nil {
		t.Fatal(err)
	}

	c := &models.Channel{ID: 1, Name: "Test"}
	cu := withOwnLevel(&models.ChannelURL{
		URL:             ownURL,
		ChanURLSettings: &models.Settings{VideoDir: "/videos/{{channel_name}}", JSONDir: "/json"},
		ChanURLMetarrArgs: &models.MetarrArgs{
			OutputDir:           "/out",
			MetaOps:             metaOps,
			FilteredMetaOps:     filteredMetaOps,
			FilteredFilenameOps: filteredFilenameOps,
		},
	})
	lines, err := ChannelURLOverrideLines(cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
	if err != nil {
		t.Fatal(err)
	}

	want := models.OverrideLines{
		VideoDir:            "/videos/{{channel_name}}", // Shown as written, as the user would enter it.
		JSONDir:             "/json",
		OutputDir:           "/out",
		MetaOps:             "title:prefix:[A] \nartist:set:Cats",
		FilteredMetaOps:     "title:contains:cat||genre:set:Cats\ntitle:contains:cat&&duration:more-than:600||album:set:Long Cats",
		FilteredFilenameOps: "title:contains:cat&&duration:more-than:600||prefix:[LONG CAT] ",
	}
	if lines != want {
		t.Errorf("got %+v\nwant %+v", lines, want)
	}

	if parsed, err := parsing.ParseMetaOps(strings.Split(lines.MetaOps, "\n")); err != nil || len(parsed) != 2 {
		t.Errorf("meta op lines parse back as %d ops, %v, want 2", len(parsed), err)
	}
	if parsed, err := parsing.ParseFilteredMetaOps(strings.Split(lines.FilteredMetaOps, "\n")); err != nil || len(parsed) != 2 || len(parsed[1].Filters) != 2 {
		t.Errorf("filtered meta op lines parse back as %+v, %v, want 2 ops, the second with 2 filters", parsed, err)
	}
	if parsed, err := parsing.ParseFilteredFilenameOps(strings.Split(lines.FilteredFilenameOps, "\n")); err != nil || len(parsed) != 1 || len(parsed[0].Filters) != 2 {
		t.Errorf("filtered filename op lines parse back as %+v, %v, want 1 op with 2 filters", parsed, err)
	}
}

// TestVideoOverrideLinesJoinedFilters tests saved Download As filtered ops refill the dialog as one line per entry, with
// all its filters and its ops joined by "&&".
func TestVideoOverrideLinesJoinedFilters(t *testing.T) {
	logger.Pl.Console = io.Discard

	cat := models.Filters{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}
	long := models.Filters{Field: "duration", FilterType: "more-than", Value: "600", MustAny: "must"}
	o := &models.VideoOverrides{MetarrArgs: models.MetarrArgs{
		FilteredMetaOps: []models.FilteredMetaOps{{
			Filters: []models.Filters{cat, long},
			MetaOps: []models.MetaOps{{Field: "album", OpType: "set", OpValue: "Long Cats"}, {Field: "genre", OpType: "set", OpValue: "Cats"}},
		}},
		FilteredFilenameOps: []models.FilteredFilenameOps{{
			Filters:     []models.Filters{cat, long},
			FilenameOps: []models.FilenameOps{{OpType: "prefix", OpValue: "[LONG CAT] "}},
		}},
	}}

	lines := VideoOverrideLines(o)
	if want := "title:contains:cat&&duration:more-than:600||album:set:Long Cats&&genre:set:Cats"; lines.FilteredMetaOps != want {
		t.Errorf("filtered meta ops = %q, want %q", lines.FilteredMetaOps, want)
	}
	if want := "title:contains:cat&&duration:more-than:600||prefix:[LONG CAT] "; lines.FilteredFilenameOps != want {
		t.Errorf("filtered filename ops = %q, want %q", lines.FilteredFilenameOps, want)
	}
}

// TestFileOverrideLines tests a Download As box gets an ops file's ops, with template tags filled in the path.
func TestFileOverrideLines(t *testing.T) {
	logger.Pl.Console = io.Discard
	dir := filepath.Join(t.TempDir(), "Test")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(filepath.Dir(dir), "{{channel_name}}", name)
	}
	c := &models.Channel{ID: 1, Name: "Test"}
	cu := &models.ChannelURL{URL: ownURL, ChanURLMetarrArgs: &models.MetarrArgs{}}
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	for _, tc := range []struct {
		box, content string
		want         []string
	}{
		{"meta_ops", "title:prefix:[A] \nartist:set:Cats\n", []string{"title:prefix:[A] ", "artist:set:Cats"}},
		{"filtered_meta_ops", "title:contains:cat||genre:set:Cats\ntitle:contains:cat&&duration:more-than:600||album:set:Long Cats\n",
			[]string{"title:contains:cat||genre:set:Cats", "title:contains:cat&&duration:more-than:600||album:set:Long Cats"}},
		{"filename_ops", "date-tag:prefix:ymd\n", []string{"date-tag:prefix:ymd"}},
		{"filtered_filename_ops", "title:contains:cat||prefix:[Cats] \ntitle:contains:cat&&duration:more-than:600||suffix: (long)\n",
			[]string{"title:contains:cat||prefix:[Cats] ", "title:contains:cat&&duration:more-than:600||suffix: (long)"}},
	} {
		got, err := FileOverrideLines(tc.box, write(tc.box+".txt", tc.content), dp)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, %v, want %q", tc.box, got, err, tc.want)
		}
	}

	if _, err := FileOverrideLines("meta_ops", write("bad.txt", "artist:bogus:x\n"), dp); err == nil {
		t.Error("expected an error for an invalid op")
	}
	if _, err := FileOverrideLines("meta_ops", write("scoped.txt", ownURL+"|title:prefix:[A] \n"), dp); err == nil {
		t.Error("expected an error for an op with a channel URL")
	}
	if _, err := FileOverrideLines("meta_ops", filepath.Join(dir, "missing.txt"), dp); err == nil {
		t.Error("expected an error for a missing file")
	}
	if _, err := FileOverrideLines("ops", write("ops.txt", "artist:set:Cats\n"), dp); err == nil {
		t.Error("expected an error for an unknown box")
	}
}

// TestParseMetarrOutputDirOverride tests a Download As output directory comes before move ops and the channel URL's.
func TestParseMetarrOutputDirOverride(t *testing.T) {
	logger.Pl.Console = io.Discard

	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{URL: ownURL, ChanURLMetarrArgs: &models.MetarrArgs{OutputDir: "/out"}}
	v := &models.Video{Files: models.VideoFiles{MoveOpOutputDir: "/moved"}, Overrides: &models.VideoOverrides{OutputDir: "/chosen/{{channel_name}}"}}
	if got := parseMetarrOutputDir(v, cu, parsing.NewDirectoryParser(c, cu, parsing.AllTags)); got != "/chosen/Cats" {
		t.Errorf("got %q, want /chosen/Cats", got)
	}

	// Output directory tags fill from the channel URL's directory.
	v.Overrides.OutputDir = "{{channel_url_output_directory}}/{{channel_name}}"
	if got := parseMetarrOutputDir(v, cu, parsing.NewDirectoryParser(c, cu, parsing.AllTags)); got != "/out/Cats" {
		t.Errorf("got %q, want /out/Cats", got)
	}
}

// TestOverrideFilenameOps tests a chosen filename replaces every other set op, and only matched filtered ops are added.
func TestOverrideFilenameOps(t *testing.T) {
	logger.Pl.Console = io.Discard

	parse := func(ops ...string) []models.FilenameOps {
		t.Helper()
		parsed, err := parsing.ParseFilenameOps(ops)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	v := &models.Video{
		URL:       "https://example.com/v/1",
		Overrides: &models.VideoOverrides{Filename: "My: Name", FilenameOps: parse("set:{{title}}", "date-tag:prefix:ymd")},
		FilteredFilenameOps: []models.FilteredFilenameOps{
			{FiltersMatched: true, FilenameOps: parse("set:{{id}}")},
			{FiltersMatched: false, FilenameOps: parse("suffix: [X]")},
		},
	}

	got := parse(overrideFilenameOps(v)...)
	var sets []string
	for _, op := range got {
		if op.OpType == sharedconsts.OpSet {
			sets = append(sets, op.OpValue)
		}
	}
	if !slices.Equal(sets, []string{"My: Name"}) {
		t.Errorf("set ops: got %q, want only the chosen filename", sets)
	}
	if len(got) != 2 || got[0].OpType != sharedconsts.OpDateTag {
		t.Errorf("got %+v, want the date tag and the chosen filename", got)
	}
}

// TestMatchedFilteredOpsBeatPlain tests a matched filtered op replaces a plain op Metarr can't run alongside it, so
// Metarr gets one filename set and the filtered artist, while other plain ops stay.
func TestMatchedFilteredOpsBeatPlain(t *testing.T) {
	logger.Pl.Console = io.Discard
	parseFilename := func(ops ...string) []models.FilenameOps {
		t.Helper()
		parsed, err := parsing.ParseFilenameOps(ops)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	parseMeta := func(ops ...string) []models.MetaOps {
		t.Helper()
		parsed, err := parsing.ParseMetaOps(ops)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	cu := withOwnLevel(&models.ChannelURL{URL: ownURL, ChanURLMetarrArgs: &models.MetarrArgs{
		FilenameOps: parseFilename("set:Plain", "prefix:[P] "),
		MetaOps:     parseMeta("artist:set:Plain", "title:suffix: [P]"),
	}})
	c := &models.Channel{ID: 1, Name: "Test"}
	v := &models.Video{
		URL:                 "https://example.com/v/1",
		FilteredFilenameOps: []models.FilteredFilenameOps{{FiltersMatched: true, FilenameOps: parseFilename("set:Filtered")}},
		FilteredMetaOps:     []models.FilteredMetaOps{{FiltersMatched: true, MetaOps: parseMeta("artist:set:Filtered")}},
	}
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	if got, want := loadAndMergeFilenameOps(v, cu, dp), []string{"prefix:[P] ", "set:Filtered"}; !slices.Equal(got, want) {
		t.Errorf("filename ops = %q, want %q", got, want)
	}
	if got, want := loadAndMergeMetaOps(v, cu, dp), []string{"title:suffix: [P]", "artist:set:Filtered"}; !slices.Equal(got, want) {
		t.Errorf("meta ops = %q, want %q", got, want)
	}
}

// TestMakeMetarrCommandOverrides tests a video's overrides replace the channel URL's ops, even when empty.
func TestMakeMetarrCommandOverrides(t *testing.T) {
	logger.Pl.Console = io.Discard

	channelOps, err := parsing.ParseMetaOps([]string{"artist:set:Channel"})
	if err != nil {
		t.Fatal(err)
	}
	c := &models.Channel{ID: 1, Name: "Test"}
	cu := withOwnLevel(&models.ChannelURL{URL: ownURL, ChanURLMetarrArgs: &models.MetarrArgs{MetaOps: channelOps}})
	v := &models.Video{URL: "https://example.com/v/1", Files: models.VideoFiles{VideoFilePath: "/v.mp4", JSONFilePath: "/v.json"}, Overrides: &models.VideoOverrides{}}

	args := makeMetarrCommand(v, cu, c, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
	for _, unwanted := range []string{"artist:set:Channel"} {
		if slices.Contains(args, unwanted) {
			t.Errorf("args %q hold %q, which the overrides replace", args, unwanted)
		}
	}
}
