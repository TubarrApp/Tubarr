package repo

import (
	"reflect"
	"testing"
	"tubarr/internal/models"
)

// fullSettings returns Settings with every field set.
func fullSettings() *models.Settings {
	yes := true
	return &models.Settings{
		DownloadConcurrency:       2,
		CookiesFromBrowser:        "firefox",
		CrawlFreq:                 30,
		ExternalDownloader:        "aria2c",
		ExternalDownloaderArgs:    "-x 4",
		MaxFilesize:               "1G",
		Retries:                   3,
		UseGlobalCookies:          &yes,
		YtdlpOutputExt:            "mp4",
		YTDLPPreferredVideoCodecs: []string{"av1"},
		YTDLPPreferredAudioCodecs: []string{"opus"},
		ExtraYTDLPVideoArgs:       "--a",
		ExtraYTDLPMetaArgs:        "--b",
		ExtraYTDLPCrawlArgs:       "--c",
		Filters:                   []models.Filters{{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}},
		FilterFile:                "/filters",
		MetaFilterMoveOps:         []models.MetaFilterMoveOps{{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}}, OutputDir: "/cats"}},
		MetaFilterMoveOpFile:      "/moves",
		FromDate:                  "20240101",
		ToDate:                    "20250101",
		JSONDir:                   "/json",
		VideoDir:                  "/video",
		Paused:                    true,
		AutoDownload:              &yes,
	}
}

// fullMetarrArgs returns MetarrArgs with every field set.
func fullMetarrArgs() *models.MetarrArgs {
	cat := models.Filters{Field: "title", FilterType: "contains", Value: "cat"}
	yes := true
	return &models.MetarrArgs{
		OutputExt:               "mp4",
		PurgeOriginalVideo:      &yes,
		FilenameOps:             []models.FilenameOps{{OpType: "prefix", OpValue: "[A] "}},
		FilenameOpsFile:         "/filename-ops",
		FilteredFilenameOps:     []models.FilteredFilenameOps{{Filters: []models.Filters{cat}, FilenameOps: []models.FilenameOps{{OpType: "prefix", OpValue: "[C] "}}}},
		FilteredFilenameOpsFile: "/filtered-filename-ops",
		RenameStyle:             "spaces",
		MetaOps:                 []models.MetaOps{{Field: "artist", OpType: "set", OpValue: "A"}},
		MetaOpsFile:             "/meta-ops",
		FilteredMetaOps:         []models.FilteredMetaOps{{Filters: []models.Filters{cat}, MetaOps: []models.MetaOps{{Field: "artist", OpType: "set", OpValue: "C"}}}},
		FilteredMetaOpsFile:     "/filtered-meta-ops",
		OutputDir:               "/out",
		MetaPurge:               "all",
		MetarrConcurrency:       2,
		MaxCPU:                  50,
		MinFreeMem:              "1G",
		TranscodeGPU:            "vaapi",
		TranscodeGPUNode:        "/dev/dri/renderD128",
		TranscodeVideoCodecs:    []string{"h264"},
		TranscodeAudioCodecs:    []string{"aac"},
		TranscodeQuality:        "23",
		ExtraFFmpegArgs:         "-x",
	}
}

// requireAllSet fails if any field of the struct v points to is unset, so a new settings field must be added here.
func requireAllSet(t *testing.T, v any) {
	t.Helper()
	rv := reflect.ValueOf(v).Elem()
	for i := range rv.NumField() {
		if rv.Field(i).IsZero() {
			t.Fatalf("%s.%s isn't set in the test's full model", rv.Type().Name(), rv.Type().Field(i).Name)
		}
	}
}

// TestMergeSettingsInheritsAll tests a channel URL without settings of its own inherits every channel setting but Paused.
func TestMergeSettingsInheritsAll(t *testing.T) {
	parent := fullSettings()
	requireAllSet(t, parent)

	child := &models.Settings{}
	mergeSettings(child, parent)
	want := *fullSettings()
	want.Paused = false
	if !reflect.DeepEqual(*child, want) {
		t.Errorf("inherited settings differ:\ngot  %+v\nwant %+v", *child, want)
	}
}

// TestMergeSettingsKeepsOwn tests a channel URL keeps the settings it sets itself, its lists added to the channel's.
func TestMergeSettingsKeepsOwn(t *testing.T) {
	no := false
	ownFilter := models.Filters{Field: "title", FilterType: "omits", Value: "dog", MustAny: "any"}
	ownMove := models.MetaFilterMoveOps{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "dog", MustAny: "must"}}, OutputDir: "/dogs"}
	ownCodecs := []string{"vp9"}
	child := &models.Settings{VideoDir: "/own", Retries: 5, Filters: []models.Filters{ownFilter}, MetaFilterMoveOps: []models.MetaFilterMoveOps{ownMove},
		YTDLPPreferredVideoCodecs: ownCodecs, UseGlobalCookies: &no, Paused: true}
	mergeSettings(child, fullSettings())

	want := *fullSettings()
	want.VideoDir, want.Retries, want.UseGlobalCookies, want.YTDLPPreferredVideoCodecs = "/own", 5, &no, ownCodecs
	want.Filters = append(want.Filters, ownFilter)
	want.MetaFilterMoveOps = append([]models.MetaFilterMoveOps{ownMove}, want.MetaFilterMoveOps...)
	if !reflect.DeepEqual(*child, want) {
		t.Errorf("merged settings differ:\ngot  %+v\nwant %+v", *child, want)
	}
}

// TestMergeMetarrArgsInheritsAll tests a channel URL without Metarr args of its own inherits every channel one.
func TestMergeMetarrArgsInheritsAll(t *testing.T) {
	parent := fullMetarrArgs()
	requireAllSet(t, parent)

	child := &models.MetarrArgs{}
	mergeMetarrArgs(child, parent)
	if want := *fullMetarrArgs(); !reflect.DeepEqual(*child, want) {
		t.Errorf("inherited Metarr args differ:\ngot  %+v\nwant %+v", *child, want)
	}
}

// TestMergeMetarrArgsKeepsOwn tests a channel URL keeps the Metarr args it sets itself, its ops added to the channel's.
func TestMergeMetarrArgsKeepsOwn(t *testing.T) {
	ownOp := models.MetaOps{Field: "title", OpType: "prefix", OpValue: "[B] "}
	ownFilenameOp := models.FilenameOps{OpType: "suffix", OpValue: " [B]"}
	ownFiltered := models.FilteredMetaOps{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "dog"}},
		MetaOps: []models.MetaOps{{Field: "genre", OpType: "set", OpValue: "Dogs"}}}
	ownCodecs := []string{"hevc"}
	child := &models.MetarrArgs{OutputDir: "/own", MaxCPU: 25, MetaOps: []models.MetaOps{ownOp}, FilenameOps: []models.FilenameOps{ownFilenameOp},
		FilteredMetaOps: []models.FilteredMetaOps{ownFiltered}, TranscodeVideoCodecs: ownCodecs}
	mergeMetarrArgs(child, fullMetarrArgs())

	want := *fullMetarrArgs()
	want.OutputDir, want.MaxCPU, want.TranscodeVideoCodecs = "/own", 25, ownCodecs
	want.MetaOps = append(want.MetaOps, ownOp)
	want.FilenameOps = append(want.FilenameOps, ownFilenameOp)
	want.FilteredMetaOps = append(want.FilteredMetaOps, ownFiltered)
	if !reflect.DeepEqual(*child, want) {
		t.Errorf("merged Metarr args differ:\ngot  %+v\nwant %+v", *child, want)
	}
}

// TestMergeOpsReplaceConflicting tests a channel URL's ops replace the channel's that Metarr can't run alongside them.
func TestMergeOpsReplaceConflicting(t *testing.T) {
	parent := &models.MetarrArgs{
		MetaOps: []models.MetaOps{
			{Field: "artist", OpType: "set", OpValue: "Channel"},
			{Field: "title", OpType: "suffix", OpValue: " [C]"},
		},
		FilenameOps: []models.FilenameOps{
			{OpType: "set", OpValue: "{{title}}"},
			{OpType: "prefix", OpValue: "[C] "},
		},
	}
	child := &models.MetarrArgs{
		MetaOps:     []models.MetaOps{{Field: "artist", OpType: "set", OpValue: "URL"}, {Field: "title", OpType: "suffix", OpValue: " [U]"}},
		FilenameOps: []models.FilenameOps{{OpType: "set", OpValue: "{{fulltitle}}"}},
	}
	mergeMetarrArgs(child, parent)

	wantMeta := []models.MetaOps{
		{Field: "title", OpType: "suffix", OpValue: " [C]"},
		{Field: "artist", OpType: "set", OpValue: "URL"},
		{Field: "title", OpType: "suffix", OpValue: " [U]"},
	}
	wantFilename := []models.FilenameOps{{OpType: "prefix", OpValue: "[C] "}, {OpType: "set", OpValue: "{{fulltitle}}"}}
	if !reflect.DeepEqual(child.MetaOps, wantMeta) {
		t.Errorf("meta ops = %+v, want %+v", child.MetaOps, wantMeta)
	}
	if !reflect.DeepEqual(child.FilenameOps, wantFilename) {
		t.Errorf("filename ops = %+v, want %+v", child.FilenameOps, wantFilename)
	}
}

// TestMergeWithoutParent tests merging with no channel settings leaves a channel URL's own unchanged.
func TestMergeWithoutParent(t *testing.T) {
	s, m := &models.Settings{VideoDir: "/own"}, &models.MetarrArgs{OutputDir: "/own"}
	mergeSettings(s, nil)
	mergeMetarrArgs(m, nil)
	if !reflect.DeepEqual(*s, models.Settings{VideoDir: "/own"}) || !reflect.DeepEqual(*m, models.MetarrArgs{OutputDir: "/own"}) {
		t.Errorf("merging with no channel settings changed them: %+v, %+v", *s, *m)
	}
}
