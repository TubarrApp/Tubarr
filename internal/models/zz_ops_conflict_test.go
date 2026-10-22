package models

import (
	"reflect"
	"testing"
)

// TestFilterConflictingMetaOps tests base ops are dropped only when an override op of a conflicting type has their field.
func TestFilterConflictingMetaOps(t *testing.T) {
	base := []MetaOps{
		{Field: "artist", OpType: "set", OpValue: "Base"},
		{Field: "album", OpType: "set", OpValue: "Base"},
		{Field: "title", OpType: "append", OpValue: " [Base]"},
	}
	overrides := []MetaOps{
		{Field: "artist", OpType: "set", OpValue: "Override"},
		{Field: "title", OpType: "append", OpValue: " [Override]"},
	}
	want := []MetaOps{base[1], base[2]}
	if got := FilterConflictingMetaOps(overrides, base); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestFilterConflictingFilenameOps tests base ops Metarr runs once are dropped when the overrides have one of that type.
func TestFilterConflictingFilenameOps(t *testing.T) {
	base := []FilenameOps{
		{OpType: "set", OpValue: "{{title}}"},
		{OpType: "date-tag", OpLoc: "prefix", DateFormat: "ymd"},
		{OpType: "prefix", OpValue: "[Base] "},
	}
	overrides := []FilenameOps{
		{OpType: "set", OpValue: "{{fulltitle}}"},
		{OpType: "prefix", OpValue: "[Override] "},
	}
	want := []FilenameOps{base[1], base[2]}
	if got := FilterConflictingFilenameOps(overrides, base); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
