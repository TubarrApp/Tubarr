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
	cu := &models.ChannelURL{
		URL:               "https://example.com/@cats",
		ChanURLSettings:   &models.Settings{FilterFile: filterFile},
		ChanURLMetarrArgs: &models.MetarrArgs{},
	}
	newVideo := func() *models.Video {
		return &models.Video{URL: "https://example.com/watch/1", JSONFilePath: jsonFile}
	}
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	// Missing file: skipped with an error.
	if pass, _, _, err := ValidateAndFilter(newVideo(), cu, c, dp); pass || !errors.Is(err, errFilterFile) {
		t.Errorf("missing filter file: passed = %v, err = %v; want not passed with errFilterFile", pass, err)
	}

	// Unparseable file: the same.
	if err := os.WriteFile(filterFile, []byte("not a filter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pass, _, _, err := ValidateAndFilter(newVideo(), cu, c, dp); pass || !errors.Is(err, errFilterFile) {
		t.Errorf("unparseable filter file: passed = %v, err = %v; want not passed with errFilterFile", pass, err)
	}

	// Chosen manually, so download filters (and the file) don't apply.
	v := newVideo()
	v.SkipFilters = true
	if pass, _, _, err := handleFilters(v, cu, c, dp); !pass || err != nil {
		t.Errorf("manual video: passed = %v, err = %v; want passed", pass, err)
	}

	// Empty file: no filters, so it passes.
	if err := os.WriteFile(filterFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if pass, _, _, err := handleFilters(newVideo(), cu, c, dp); !pass || err != nil {
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
		v := &models.Video{URL: "https://example.com/watch/1", MetadataMap: map[string]any{"title": tt.title}}
		if got := checkFilters(v, "Download filters", tt.filters); got != tt.want {
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
		v := &models.Video{URL: "https://example.com/watch/1", MetadataMap: meta}
		if got := checkFilters(v, "Download filters", []models.Filters{tt.filter}); got != tt.want {
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
		v := &models.Video{URL: "https://example.com/watch/1", MetadataMap: tt.meta}
		if got := checkFilters(v, "Download filters", tt.filters); got != tt.want {
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
		v := &models.Video{URL: "https://example.com/watch/1", MetadataMap: tt.meta}
		if got := filteredMetaOpsMatches(v, cu, metaOps, "Cats"); len(got) != 1 || got[0].FiltersMatched != tt.want {
			t.Errorf("%s: filtered meta ops got %+v, want matched %v", tt.name, got, tt.want)
		}
		if got := filteredFilenameOpsMatches(v, cu, filenameOps, "Cats"); len(got) != 1 || got[0].FiltersMatched != tt.want {
			t.Errorf("%s: filtered filename ops got %+v, want matched %v", tt.name, got, tt.want)
		}
	}
}

// TestHandleMoveOps tests move ops pick the first op whose filters all match, by any filter type, for their channel URL.
func TestHandleMoveOps(t *testing.T) {
	logger.Pl.Console = io.Discard
	video := &models.Video{MetadataMap: map[string]any{"title": "Two Cats Playing", "duration": 4000}}

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
		cu := &models.ChannelURL{URL: "https://tubesite.com/@cats", ChanURLSettings: &models.Settings{MetaFilterMoveOps: ops}}
		if got := HandleMoveOps(video, cu, nil); got != tt.want {
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

	v := &models.Video{URL: "https://example.com/v/1", JSONFilePath: path}
	if valid, err := parseAndStoreJSON(v); err != nil || !valid {
		t.Fatalf("valid %t, err %v", valid, err)
	}
	if v.Title != "Full" || v.Description != "Long desc" || v.UploadDate.Format("2006-01-02") != "2024-01-02" {
		t.Errorf("got title %q, description %q, date %v", v.Title, v.Description, v.UploadDate)
	}

	v = &models.Video{URL: "https://example.com/v/1", JSONFilePath: path, Title: "Kept", Description: "Kept"}
	if _, err := parseAndStoreJSON(v); err != nil {
		t.Fatal(err)
	}
	if v.Title != "Kept" || v.Description != "Kept" {
		t.Errorf("stored values replaced: got title %q, description %q", v.Title, v.Description)
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
