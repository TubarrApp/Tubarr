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
		MaxResolution:             "1080",
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
		FiltersReplaceInherited:   true,
		MoveOpsReplaceInherited:   true,
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
		WriteNFO:                &yes,
		OutputDir:               "/out",
		MetaPurge:               "all",
		MetarrConcurrency:       2,
		MaxCPU:                  50,
		MinFreeMem:              "1G",
		TranscodeGPU:            "vaapi",
		TranscodeGPUNode:        "/dev/dri/renderD128",
		TranscodeVideoCodecs:    []string{"h264"},
		TranscodeAudioCodecs:    []string{"aac"},
		TranscodeVideoArgs:      []string{"hevc:-crf 23"},
		ExtraFFmpegArgs:         "-x",

		MetaOpsReplaceInherited:             true,
		FilteredMetaOpsReplaceInherited:     true,
		FilenameOpsReplaceInherited:         true,
		FilteredFilenameOpsReplaceInherited: true,
	}
}

// withoutReplaceTicks returns m with its replace ticks cleared, as merging never passes them on.
func withoutReplaceTicks(m *models.MetarrArgs) models.MetarrArgs {
	m.MetaOpsReplaceInherited = false
	m.FilteredMetaOpsReplaceInherited = false
	m.FilenameOpsReplaceInherited = false
	m.FilteredFilenameOpsReplaceInherited = false
	return *m
}

// withoutOpLists returns m without its op lists, which merging leaves to stackLists.
func withoutOpLists(m models.MetarrArgs) models.MetarrArgs {
	m.MetaOps = nil
	m.FilenameOps = nil
	m.FilteredMetaOps = nil
	m.FilteredFilenameOps = nil
	return m
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

// TestMergeSettingsInheritsAll tests a channel URL without settings of its own inherits every channel setting but
// Paused, the replace ticks and the lists, which stackLists fills from every level.
func TestMergeSettingsInheritsAll(t *testing.T) {
	parent := fullSettings()
	requireAllSet(t, parent)

	child := &models.Settings{}
	mergeSettings(child, parent)
	want := *fullSettings()
	want.Paused = false
	want.FiltersReplaceInherited = false
	want.MoveOpsReplaceInherited = false
	want.Filters = nil
	want.MetaFilterMoveOps = nil
	if !reflect.DeepEqual(*child, want) {
		t.Errorf("inherited settings differ:\ngot  %+v\nwant %+v", *child, want)
	}
}

// TestMergeSettingsKeepsOwn tests a channel URL keeps the settings it sets itself, leaving its lists to stackLists.
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
	want.FiltersReplaceInherited = false
	want.MoveOpsReplaceInherited = false
	want.Filters = []models.Filters{ownFilter}
	want.MetaFilterMoveOps = []models.MetaFilterMoveOps{ownMove}
	if !reflect.DeepEqual(*child, want) {
		t.Errorf("merged settings differ:\ngot  %+v\nwant %+v", *child, want)
	}
}

// TestMergeMetarrArgsInheritsAll tests a channel URL without Metarr args of its own inherits every channel one but the
// replace ticks and the op lists, which stackLists fills from every level.
func TestMergeMetarrArgsInheritsAll(t *testing.T) {
	parent := fullMetarrArgs()
	requireAllSet(t, parent)

	child := &models.MetarrArgs{}
	mergeMetarrArgs(child, parent)
	if want := withoutOpLists(withoutReplaceTicks(fullMetarrArgs())); !reflect.DeepEqual(*child, want) {
		t.Errorf("inherited Metarr args differ:\ngot  %+v\nwant %+v", *child, want)
	}
}

// TestMergeMetarrArgsKeepsOwn tests a channel URL keeps the Metarr args it sets itself, leaving its op lists to
// stackLists.
func TestMergeMetarrArgsKeepsOwn(t *testing.T) {
	ownOp := models.MetaOps{Field: "title", OpType: "prefix", OpValue: "[B] "}
	ownFilenameOp := models.FilenameOps{OpType: "suffix", OpValue: " [B]"}
	ownFiltered := models.FilteredMetaOps{Filters: []models.Filters{{Field: "title", FilterType: "contains", Value: "dog"}},
		MetaOps: []models.MetaOps{{Field: "genre", OpType: "set", OpValue: "Dogs"}}}
	ownCodecs := []string{"hevc"}
	child := &models.MetarrArgs{OutputDir: "/own", MaxCPU: 25, MetaOps: []models.MetaOps{ownOp}, FilenameOps: []models.FilenameOps{ownFilenameOp},
		FilteredMetaOps: []models.FilteredMetaOps{ownFiltered}, TranscodeVideoCodecs: ownCodecs}
	mergeMetarrArgs(child, fullMetarrArgs())

	want := withoutReplaceTicks(fullMetarrArgs())
	want.OutputDir, want.MaxCPU, want.TranscodeVideoCodecs = "/own", 25, ownCodecs
	want.MetaOps = []models.MetaOps{ownOp}
	want.FilenameOps = []models.FilenameOps{ownFilenameOp}
	want.FilteredMetaOps = []models.FilteredMetaOps{ownFiltered}
	want.FilteredFilenameOps = nil
	if !reflect.DeepEqual(*child, want) {
		t.Errorf("merged Metarr args differ:\ngot  %+v\nwant %+v", *child, want)
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
