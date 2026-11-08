package metadata

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
)

// withOwnLevel gives a channel URL built in a test its own settings as its only level, as loading it would.
func withOwnLevel(cu *models.ChannelURL) *models.ChannelURL {
	cu.Levels = []models.SettingsLevel{{Settings: cu.ChanURLSettings, MetarrArgs: cu.ChanURLMetarrArgs}}
	return cu
}

// TestUnusableFilterFileSkipsVideo tests a filter file that can't be read stops a video downloading, as an error so it
// isn't marked ignored, rather than letting it through unfiltered.
func TestUnusableFilterFileSkipsVideo(t *testing.T) {
	logger.Pl.Console = io.Discard
	dir := t.TempDir()
	filterFile := filepath.Join(dir, "filters.txt")
	jsonFile := filepath.Join(dir, "video.json")
	if err := os.WriteFile(jsonFile, []byte(`{"title":"A video"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := withOwnLevel(&models.ChannelURL{
		URL:               "https://example.com/@cats",
		ChanURLSettings:   &models.Settings{FilterFile: filterFile},
		ChanURLMetarrArgs: &models.MetarrArgs{},
	})
	newVideo := func() *models.Video {
		return &models.Video{URL: "https://example.com/watch/1", Files: models.VideoFiles{JSONFilePath: jsonFile}}
	}
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	// Missing file: skipped with an error.
	if pass, _, _, err := ValidateAndFilter(newVideo(), cu, dp); pass || !errors.Is(err, errFilterFile) {
		t.Errorf("missing filter file: passed = %v, err = %v; want not passed with errFilterFile", pass, err)
	}

	// Unparseable file: the same.
	if err := os.WriteFile(filterFile, []byte("not a filter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pass, _, _, err := ValidateAndFilter(newVideo(), cu, dp); pass || !errors.Is(err, errFilterFile) {
		t.Errorf("unparseable filter file: passed = %v, err = %v; want not passed with errFilterFile", pass, err)
	}

	// Chosen manually, so download filters (and the file) don't apply.
	v := newVideo()
	v.SkipFilters = true
	if pass, _, _, err := handleFilters(v, cu, dp); !pass || err != nil {
		t.Errorf("manual video: passed = %v, err = %v; want passed", pass, err)
	}

	// Empty file: no filters, so it passes.
	if err := os.WriteFile(filterFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if pass, _, _, err := handleFilters(newVideo(), cu, dp); !pass || err != nil {
		t.Errorf("empty filter file: passed = %v, err = %v; want passed", pass, err)
	}
}

// TestMustAndAnyFilters tests a video passes when every 'must' filter passes and, if there are 'any' filters, at least
// one of them does.
func TestMustAndAnyFilters(t *testing.T) {
	logger.Pl.Console = io.Discard
	f := func(op, value, mustAny string) models.Filters {
		return models.Filters{Field: "title", FilterType: op, Value: value, MustAny: mustAny}
	}
	tutorial := f("contains", "tutorial", "must")
	beginner, intro := f("contains", "beginner", "any"), f("contains", "intro", "any")
	tests := []struct {
		title   string
		filters []models.Filters
		want    bool
	}{
		{"tutorial for beginners", []models.Filters{tutorial, beginner, intro}, true},
		{"advanced tutorial", []models.Filters{tutorial, beginner, intro}, false}, // No 'any' filter passed.
		{"intro for beginners", []models.Filters{tutorial, beginner, intro}, false},
		{"intro", []models.Filters{beginner, intro}, true},
		{"advanced", []models.Filters{beginner, intro}, false},
		{"tutorial", []models.Filters{tutorial}, true},
	}
	for _, tt := range tests {
		v := &models.Video{URL: "https://example.com/watch/1", Metadata: models.VideoMetadata{Raw: map[string]any{"title": tt.title}}}
		if got := checkFilters(v, tt.filters); got != tt.want {
			t.Errorf("checkFilters(%q, %v) = %v, want %v", tt.title, tt.filters, got, tt.want)
		}
	}
}

// TestCheckFiltersFieldValues tests download filters on durations, empty values, and missing fields.
func TestCheckFiltersFieldValues(t *testing.T) {
	logger.Pl.Console = io.Discard
	meta := map[string]any{"title": "Cats", "description": "", "duration_string": "7:24:44"}
	tests := []struct {
		name   string
		filter models.Filters
		want   bool
	}{
		{"duration string more-than", models.Filters{Field: "duration_string", FilterType: "more-than", Value: "3600", MustAny: "must"}, true},
		{"duration string less-than", models.Filters{Field: "duration_string", FilterType: "less-than", Value: "3600", MustAny: "must"}, false},
		{"missing numeric field is skipped", models.Filters{Field: "duration", FilterType: "more-than", Value: "3600", MustAny: "must"}, true},
		{"text field fails a numeric filter", models.Filters{Field: "title", FilterType: "more-than", Value: "1", MustAny: "must"}, false},
		{"equals empty", models.Filters{Field: "description", FilterType: "equals", MustAny: "must"}, true},
		{"not-equals empty", models.Filters{Field: "title", FilterType: "not-equals", MustAny: "must"}, true},
		{"missing field equals empty", models.Filters{Field: "absent", FilterType: "equals", MustAny: "must"}, true},
	}
	for _, tt := range tests {
		v := &models.Video{URL: "https://example.com/watch/1", Metadata: models.VideoMetadata{Raw: meta}}
		if got := checkFilters(v, []models.Filters{tt.filter}); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestPresenceAnyFilters tests contains and omits 'any' filters with no value act as ordinary 'any' filters on whether
// the field is present.
func TestPresenceAnyFilters(t *testing.T) {
	logger.Pl.Console = io.Discard
	hasDate := models.Filters{Field: "date", FilterType: "contains", MustAny: "any"}
	noDate := models.Filters{Field: "date", FilterType: "omits", MustAny: "any"}
	cats := models.Filters{Field: "title", FilterType: "contains", Value: "cats", MustAny: "any"}
	tests := []struct {
		name    string
		meta    map[string]any
		filters []models.Filters
		want    bool
	}{
		{"no date but cats", map[string]any{"title": "Cats"}, []models.Filters{hasDate, cats}, true},
		{"no date and no cats", map[string]any{"title": "Dogs"}, []models.Filters{hasDate, cats}, false},
		{"date but no cats", map[string]any{"title": "Dogs", "date": "2024-01-02"}, []models.Filters{hasDate, cats}, true},
		{"omits date", map[string]any{"title": "Dogs"}, []models.Filters{noDate}, true},
		{"date and no cats", map[string]any{"title": "Dogs", "date": "2024-01-02"}, []models.Filters{noDate, cats}, false},
	}
	for _, tt := range tests {
		v := &models.Video{URL: "https://example.com/watch/1", Metadata: models.VideoMetadata{Raw: tt.meta}}
		if got := checkFilters(v, tt.filters); got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, got, tt.want)
		}
	}
}

// TestFilteredOpsNumericField tests filtered ops apply by a numeric field's value, and not when it's missing.
func TestFilteredOpsNumericField(t *testing.T) {
	logger.Pl.Console = io.Discard
	cu := &models.ChannelURL{ChanURLMetarrArgs: &models.MetarrArgs{}}
	long := []models.Filters{{Field: "duration", FilterType: "more-than", Value: "3600", MustAny: "must"}}
	metaOps := []models.FilteredMetaOps{{Filters: long, MetaOps: []models.MetaOps{{Field: "title", OpType: "prefix", OpValue: "[LONG] "}}}}
	filenameOps := []models.FilteredFilenameOps{{Filters: long, FilenameOps: []models.FilenameOps{{OpType: "prefix", OpValue: "[LONG] "}}}}

	for _, tt := range []struct {
		name string
		meta map[string]any
		want bool
	}{
		{"missing", map[string]any{"title": "Cats"}, false},
		{"short", map[string]any{"title": "Cats", "duration": 60.0}, false},
		{"long", map[string]any{"title": "Cats", "duration": 4000.0}, true},
		{"long duration string", map[string]any{"title": "Cats", "duration": "1:06:40"}, true},
	} {
		v := &models.Video{URL: "https://example.com/watch/1", Metadata: models.VideoMetadata{Raw: tt.meta}}
		if got := filteredMetaOpsMatches(v, cu, [][]models.FilteredMetaOps{metaOps}); len(got) != 1 || got[0].FiltersMatched != tt.want {
			t.Errorf("%s: filtered meta ops got %+v, want matched %v", tt.name, got, tt.want)
		}
		if got := filteredFilenameOpsMatches(v, cu, [][]models.FilteredFilenameOps{filenameOps}); len(got) != 1 || got[0].FiltersMatched != tt.want {
			t.Errorf("%s: filtered filename ops got %+v, want matched %v", tt.name, got, tt.want)
		}
	}
}

// TestMatchedFilteredOpsNearerThenFirstWin tests that of matched filtered ops setting the same thing, a nearer level's
// wins, then the earlier line's in the same level, and that one matching a plain op still claims what it sets.
func TestMatchedFilteredOpsNearerThenFirstWin(t *testing.T) {
	logger.Pl.Console = io.Discard
	cat := []models.Filters{{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"}}
	setName := func(v string) models.FilteredFilenameOps {
		return models.FilteredFilenameOps{Filters: cat, FilenameOps: []models.FilenameOps{{OpType: "set", OpValue: v}}}
	}
	setArtist := func(v string) models.FilteredMetaOps {
		return models.FilteredMetaOps{Filters: cat, MetaOps: []models.MetaOps{{Field: "artist", OpType: "set", OpValue: v}}}
	}
	plain := []models.MetaOps{{Field: "artist", OpType: "set", OpValue: "Plain"}}
	cu := &models.ChannelURL{ChanURLMetarrArgs: &models.MetarrArgs{MetaOps: plain}}
	v := &models.Video{URL: "https://example.com/watch/1", Metadata: models.VideoMetadata{Raw: map[string]any{"title": "cat"}}}

	// Filename: the profile's set loses to the channel's, whose first line wins.
	var gotNames []string
	for _, ffo := range filteredFilenameOpsMatches(v, cu, [][]models.FilteredFilenameOps{{setName("Profile")}, {setName("First"), setName("Second")}}) {
		for _, fo := range ffo.FilenameOps {
			gotNames = append(gotNames, fo.OpValue)
		}
	}
	if len(gotNames) != 1 || gotNames[0] != "First" {
		t.Errorf("filename sets kept = %q, want only the channel's first line", gotNames)
	}

	// Meta: the URL's matched op is the plain one, so it's dropped as a duplicate but still beats the profile's.
	var gotArtists []string
	for _, fmo := range filteredMetaOpsMatches(v, cu, [][]models.FilteredMetaOps{{setArtist("Profile")}, {setArtist("Plain")}}) {
		for _, mo := range fmo.MetaOps {
			gotArtists = append(gotArtists, mo.OpValue)
		}
	}
	if len(gotArtists) != 0 {
		t.Errorf("artist sets kept = %q, want none, leaving the plain op", gotArtists)
	}
}

// TestHandleMoveOps tests move ops pick the first op whose filters all match, by any filter type, for their channel URL.
func TestHandleMoveOps(t *testing.T) {
	logger.Pl.Console = io.Discard
	video := &models.Video{Metadata: models.VideoMetadata{Raw: map[string]any{"title": "Two Cats Playing", "duration": 4000}}}

	tests := []struct {
		name string
		ops  []string
		want string
	}{
		{"contains", []string{"title:contains:cats||/cats"}, "/cats"},
		{"no match", []string{"title:contains:dogs||/dogs"}, ""},
		{"omits", []string{"title:omits:dogs||/no-dogs"}, "/no-dogs"},
		{"more-than", []string{"duration:more-than:3600||/long"}, "/long"},
		{"missing field for a comparison", []string{"views:more-than:1||/popular"}, ""},
		{"all filters match", []string{"title:contains:cats&&duration:more-than:3600||/long cats"}, "/long cats"},
		{"one filter fails", []string{"title:contains:cats&&duration:less-than:3600||/short cats"}, ""},
		{"first match wins", []string{"title:contains:dogs||/dogs", "title:contains:two||/two", "title:contains:cats||/cats"}, "/two"},
	}
	for _, tt := range tests {
		ops, err := parsing.ParseMetaFilterMoveOps(tt.ops)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		cu := withOwnLevel(&models.ChannelURL{URL: "https://tubesite.com/@cats", ChanURLSettings: &models.Settings{MetaFilterMoveOps: ops}})
		if got := HandleMoveOps(video, cu, nil, nil); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestHandleMoveOpsOutputDirectoryTags tests both tags fill from the output directory, else the video directory, and an op with neither is skipped.
func TestHandleMoveOpsOutputDirectoryTags(t *testing.T) {
	logger.Pl.Console = io.Discard
	video := &models.Video{Metadata: models.VideoMetadata{Raw: map[string]any{"title": "Two Cats Playing"}}}

	withOutput := &models.Channel{ChanSettings: &models.Settings{VideoDir: "/channel-video"}, ChanMetarrArgs: &models.MetarrArgs{OutputDir: "/channel"}}
	videoOnly := &models.Channel{ChanSettings: &models.Settings{VideoDir: "/channel-video"}, ChanMetarrArgs: &models.MetarrArgs{}}
	noDirs := &models.Channel{}

	tests := []struct {
		name      string
		dir       string
		c         *models.Channel
		urlOutput string
		want      string
	}{
		{"channel output directory", "{{channel_output_directory}}/Shorts", withOutput, "", "/channel/Shorts"},
		{"channel video directory", "{{channel_output_directory}}/Shorts", videoOnly, "", "/channel-video/Shorts"},
		{"channel tag with a URL output directory", "{{channel_output_directory}}/Shorts", withOutput, "/url", "/channel/Shorts"},
		{"URL output directory", "{{channel_url_output_directory}}/Shorts", withOutput, "/url", "/url/Shorts"},
		{"URL video directory", "{{channel_url_output_directory}}/Shorts", withOutput, "", "/url-video/Shorts"},
		{"no channel directory", "{{channel_output_directory}}/Shorts", noDirs, "", ""},
	}
	for _, tt := range tests {
		ops, err := parsing.ParseMetaFilterMoveOps([]string{"title:contains:cats||" + tt.dir})
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		cu := withOwnLevel(&models.ChannelURL{
			URL:               "https://tubesite.com/@cats",
			ChanURLSettings:   &models.Settings{VideoDir: "/url-video", MetaFilterMoveOps: ops},
			ChanURLMetarrArgs: &models.MetarrArgs{OutputDir: tt.urlOutput},
		})
		if got := HandleMoveOps(video, cu, tt.c, nil); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

// TestParseAndStoreJSONPrecedence tests a downloaded video's title, description and date are taken by the precedence
// Metarr embeds them with, filling only those not already stored.
func TestParseAndStoreJSONPrecedence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v.json")
	body := `{"title": "Short", "fulltitle": "Full", "description": "Desc", "longdescription": "Long desc",
		"upload_date": "20240105", "release_date": "2024-01-02"}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	v := &models.Video{URL: "https://example.com/v/1", Files: models.VideoFiles{JSONFilePath: path}}
	if valid, err := parseAndStoreJSON(v); err != nil || !valid {
		t.Fatalf("valid %t, err %v", valid, err)
	}
	if v.Metadata.Title != "Full" || v.Metadata.Description != "Long desc" || v.Metadata.UploadDate.Format("2006-01-02") != "2024-01-02" {
		t.Errorf("got title %q, description %q, date %v", v.Metadata.Title, v.Metadata.Description, v.Metadata.UploadDate)
	}

	v = &models.Video{URL: "https://example.com/v/1", Files: models.VideoFiles{JSONFilePath: path}, Metadata: models.VideoMetadata{Title: "Kept", Description: "Kept"}}
	if _, err := parseAndStoreJSON(v); err != nil {
		t.Fatal(err)
	}
	if v.Metadata.Title != "Kept" || v.Metadata.Description != "Kept" {
		t.Errorf("stored values replaced: got title %q, description %q", v.Metadata.Title, v.Metadata.Description)
	}
}

// TestMetadataDate tests dates are taken by precedence, skipping ones that don't parse and ignoring times.
func TestMetadataDate(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    map[string]any
		want string
	}{
		{"release date first", map[string]any{"upload_date": "20240105", "release_date": "20240102"}, "2024-01-02"},
		{"unparsable skipped", map[string]any{"release_date": "soon", "upload_date": "2024-01-05"}, "2024-01-05"},
		{"creation time last", map[string]any{"creation_time": "2024-01-03T10:00:00Z"}, "2024-01-03"},
		{"none", map[string]any{"title": "x"}, "0001-01-01"},
	} {
		if got := metadataDate(tc.m).Format("2006-01-02"); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}

// TestMetadataDuration tests duration is preferred over duration_string, and either may be a string to parse.
func TestMetadataDuration(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    map[string]any
		want int
	}{
		{"duration", map[string]any{"duration": 312.5, "duration_string": "1:00"}, 312},
		{"duration string", map[string]any{"duration_string": "7:24:44"}, 26684},
		{"iso duration", map[string]any{"duration": "PT07H24M44S"}, 26684},
		{"zero duration falls back", map[string]any{"duration": 0.0, "duration_string": "1:00"}, 60},
		{"none", map[string]any{"title": "x"}, 0},
	} {
		if got := metadataDuration(tc.m); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}
