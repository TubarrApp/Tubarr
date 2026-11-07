package models

import (
	"reflect"
	"testing"
)

// TestStackOpsNearerWins tests a nearer level's ops replace a farther level's that Metarr can't run alongside them, and
// the rest are added together.
func TestStackOpsNearerWins(t *testing.T) {
	channel := &MetarrArgs{
		MetaOps: []MetaOps{
			{Field: "artist", OpType: "set", OpValue: "Channel"},
			{Field: "title", OpType: "suffix", OpValue: " [C]"},
		},
		FilenameOps: []FilenameOps{
			{OpType: "set", OpValue: "{{title}}"},
			{OpType: "prefix", OpValue: "[C] "},
		},
	}
	url := &MetarrArgs{
		MetaOps:     []MetaOps{{Field: "artist", OpType: "set", OpValue: "URL"}, {Field: "title", OpType: "suffix", OpValue: " [U]"}},
		FilenameOps: []FilenameOps{{OpType: "set", OpValue: "{{fulltitle}}"}},
	}
	levels := []SettingsLevel{{MetarrArgs: channel}, {MetarrArgs: url}}

	wantMeta := []MetaOps{
		{Field: "title", OpType: "suffix", OpValue: " [C]"},
		{Field: "artist", OpType: "set", OpValue: "URL"},
		{Field: "title", OpType: "suffix", OpValue: " [U]"},
	}
	wantFilename := []FilenameOps{{OpType: "prefix", OpValue: "[C] "}, {OpType: "set", OpValue: "{{fulltitle}}"}}
	if got := StackMetaOps(levels, nil); !reflect.DeepEqual(got, wantMeta) {
		t.Errorf("meta ops = %+v, want %+v", got, wantMeta)
	}
	if got := StackFilenameOps(levels, nil); !reflect.DeepEqual(got, wantFilename) {
		t.Errorf("filename ops = %+v, want %+v", got, wantFilename)
	}
}

// TestStackOpsFilesByLevel tests an ops file counts as its own level's: it beats that level's typed ops, but loses to a
// nearer level's, and every level's file is used.
func TestStackOpsFilesByLevel(t *testing.T) {
	profileTitle := MetaOps{Field: "title", OpType: "prefix", OpValue: "[P] "}
	profileFileSet := MetaOps{Field: "title", OpType: "set", OpValue: "From Profile File"}
	channelSet := MetaOps{Field: "title", OpType: "set", OpValue: "From Channel"}
	urlTyped := MetaOps{Field: "artist", OpType: "set", OpValue: "URL Typed"}
	urlFile := MetaOps{Field: "artist", OpType: "set", OpValue: "URL File"}
	levels := []SettingsLevel{
		{MetarrArgs: &MetarrArgs{MetaOps: []MetaOps{profileTitle}, FilenameOps: []FilenameOps{{OpType: "suffix", OpValue: " [P]"}}}},
		{MetarrArgs: &MetarrArgs{MetaOps: []MetaOps{channelSet}, FilenameOps: []FilenameOps{{OpType: "set", OpValue: "Channel"}}}},
		{MetarrArgs: &MetarrArgs{MetaOps: []MetaOps{urlTyped}}},
	}
	metaFiles := [][]MetaOps{{profileFileSet}, nil, {urlFile}}
	filenameFiles := [][]FilenameOps{{{OpType: "set", OpValue: "Profile File"}}, nil, nil}

	wantMeta := []MetaOps{profileTitle, channelSet, urlFile}
	if got := StackMetaOps(levels, metaFiles); !reflect.DeepEqual(got, wantMeta) {
		t.Errorf("meta ops = %+v, want %+v", got, wantMeta)
	}
	wantFilename := []FilenameOps{{OpType: "suffix", OpValue: " [P]"}, {OpType: "set", OpValue: "Channel"}}
	if got := StackFilenameOps(levels, filenameFiles); !reflect.DeepEqual(got, wantFilename) {
		t.Errorf("filename ops = %+v, want %+v", got, wantFilename)
	}
}

// TestStackFiltersAndMoveOps tests every level's filters are used, and move ops are checked nearest level first, each
// level's typed ones before its file's.
func TestStackFiltersAndMoveOps(t *testing.T) {
	filter := func(v string) Filters {
		return Filters{Field: "title", FilterType: "contains", Value: v, MustAny: "must"}
	}
	move := func(dir string) MetaFilterMoveOps { return MetaFilterMoveOps{OutputDir: dir} }
	levels := []SettingsLevel{
		{Settings: &Settings{Filters: []Filters{filter("channel")}, MetaFilterMoveOps: []MetaFilterMoveOps{move("/channel")}}},
		{Settings: nil}, // A level with no settings, such as a profile without them.
		{Settings: &Settings{Filters: []Filters{filter("url")}, MetaFilterMoveOps: []MetaFilterMoveOps{move("/url")}}},
	}
	fileFilters := [][]Filters{{filter("channel file")}, nil, {filter("url file")}}
	fileMoves := [][]MetaFilterMoveOps{{move("/channel file")}, nil, {move("/url file")}}

	wantFilters := []Filters{filter("channel"), filter("channel file"), filter("url"), filter("url file")}
	if got := StackFilters(levels, fileFilters); !reflect.DeepEqual(got, wantFilters) {
		t.Errorf("filters = %+v, want %+v", got, wantFilters)
	}
	wantMoves := []MetaFilterMoveOps{move("/url"), move("/url file"), move("/channel"), move("/channel file")}
	if got := StackMoveOps(levels, fileMoves); !reflect.DeepEqual(got, wantMoves) {
		t.Errorf("move ops = %+v, want %+v", got, wantMoves)
	}
}

// TestFilteredOpsByLevel tests filtered ops stay by level, each level's typed ones before its file's.
func TestFilteredOpsByLevel(t *testing.T) {
	entry := func(v string) FilteredMetaOps {
		return FilteredMetaOps{MetaOps: []MetaOps{{Field: "genre", OpType: "set", OpValue: v}}}
	}
	levels := []SettingsLevel{
		{MetarrArgs: &MetarrArgs{FilteredMetaOps: []FilteredMetaOps{entry("channel")}}},
		{MetarrArgs: &MetarrArgs{FilteredMetaOps: []FilteredMetaOps{entry("url")}}},
	}
	got := FilteredMetaOpsByLevel(levels, [][]FilteredMetaOps{{entry("channel file")}})
	want := [][]FilteredMetaOps{{entry("channel"), entry("channel file")}, {entry("url")}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("by level = %+v, want %+v", got, want)
	}
}
