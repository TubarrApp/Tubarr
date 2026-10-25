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

// TestHandleMoveOps tests move ops pick the first matching op's directory, by any filter type, for their channel URL.
func TestHandleMoveOps(t *testing.T) {
	logger.Pl.Console = io.Discard
	video := &models.Video{MetadataMap: map[string]any{"title": "Two Cats Playing", "duration": 4000}}

	tests := []struct {
		name string
		ops  []models.MetaFilterMoveOps
		want string
	}{
		{"saved before filter types, read as contains", []models.MetaFilterMoveOps{{Field: "title", Value: "cats", OutputDir: "/cats"}}, "/cats"},
		{"no match", []models.MetaFilterMoveOps{{Field: "title", FilterType: "contains", Value: "dogs", OutputDir: "/dogs"}}, ""},
		{"omits", []models.MetaFilterMoveOps{{Field: "title", FilterType: "omits", Value: "dogs", OutputDir: "/no-dogs"}}, "/no-dogs"},
		{"morethan", []models.MetaFilterMoveOps{{Field: "duration", FilterType: "morethan", Value: "3600", OutputDir: "/long"}}, "/long"},
		{"missing field for a comparison", []models.MetaFilterMoveOps{{Field: "views", FilterType: "morethan", Value: "1", OutputDir: "/popular"}}, ""},
		{"first match wins", []models.MetaFilterMoveOps{
			{Field: "title", FilterType: "contains", Value: "dogs", OutputDir: "/dogs"},
			{Field: "title", FilterType: "contains", Value: "two", OutputDir: "/two"},
			{Field: "title", FilterType: "contains", Value: "cats", OutputDir: "/cats"},
		}, "/two"},
	}
	for _, tt := range tests {
		cu := &models.ChannelURL{URL: "https://tubesite.com/@cats", ChanURLSettings: &models.Settings{MetaFilterMoveOps: tt.ops}}
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
