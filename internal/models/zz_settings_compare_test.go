package models

import (
	"fmt"
	"reflect"
	"testing"
)

// setNonZero sets v to a non-zero value which differs for each n.
func setNonZero(v reflect.Value, n int) {
	switch v.Kind() {
	case reflect.String:
		v.SetString(fmt.Sprint(n))
	case reflect.Int:
		v.SetInt(int64(n))
	case reflect.Float64:
		v.SetFloat(float64(n))
	case reflect.Bool:
		v.SetBool(n == 1)
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		setNonZero(p.Elem(), n)
		v.Set(p)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		setNonZero(s.Index(0), n)
		v.Set(s)
	case reflect.Struct:
		setNonZero(v.Field(0), n)
	}
}

// TestChildMatchParentEachField tests a child setting one field matches a parent with the same value there, and only that.
func TestChildMatchParentEachField(t *testing.T) {
	for _, tc := range []struct {
		model any
		match func(parent, child any) bool
	}{
		{Settings{}, func(p, c any) bool { return ChildSettingsMatchParent(p.(*Settings), c.(*Settings)) }},
		{MetarrArgs{}, func(p, c any) bool { return ChildMetarrArgsMatchParent(p.(*MetarrArgs), c.(*MetarrArgs)) }},
	} {
		typ := reflect.TypeOf(tc.model)
		for i := range typ.NumField() {
			name := typ.Name() + "." + typ.Field(i).Name
			if name == "Settings.Paused" {
				continue // Pausing is per URL, so a paused child never matches.
			}
			parent, child := reflect.New(typ), reflect.New(typ)
			setNonZero(parent.Elem().Field(i), 1)
			setNonZero(child.Elem().Field(i), 1)
			if !tc.match(parent.Interface(), child.Interface()) {
				t.Errorf("%s: the same value doesn't match", name)
			}
			setNonZero(child.Elem().Field(i), 2)
			if tc.match(parent.Interface(), child.Interface()) {
				t.Errorf("%s: a different value matches", name)
			}
		}
	}
}

// TestSettingsAllZeroCodecs tests that preferred codecs alone make Settings non-zero.
func TestSettingsAllZeroCodecs(t *testing.T) {
	if SettingsAllZero(&Settings{YTDLPPreferredVideoCodecs: []string{"av1"}}) {
		t.Error("Settings with only preferred video codecs reported as all zero")
	}
	if SettingsAllZero(&Settings{YTDLPPreferredAudioCodecs: []string{"opus"}}) {
		t.Error("Settings with only preferred audio codecs reported as all zero")
	}
}

// TestChildSettingsMatchParentLists tests that identical lists of several different entries match.
func TestChildSettingsMatchParentLists(t *testing.T) {
	filters := []Filters{
		{Field: "title", FilterType: "contains", Value: "cat", MustAny: "must"},
		{Field: "title", FilterType: "omits", Value: "dog", MustAny: "any"},
	}
	moveOps := []MetaFilterMoveOps{
		{Field: "title", Value: "cat", OutputDir: "/cats"},
		{Field: "title", Value: "dog", OutputDir: "/dogs"},
	}
	parent := &Settings{Filters: filters, MetaFilterMoveOps: moveOps}
	child := &Settings{Filters: append([]Filters(nil), filters...), MetaFilterMoveOps: append([]MetaFilterMoveOps(nil), moveOps...)}
	if !ChildSettingsMatchParent(parent, child) {
		t.Error("identical filters and move ops reported as not matching the parent")
	}

	child.Filters = filters[:1]
	if ChildSettingsMatchParent(parent, child) {
		t.Error("fewer filters reported as matching the parent")
	}
}

// TestChildMetarrArgsMatchParentLists tests that identical filtered op lists of several different entries match.
func TestChildMetarrArgsMatchParentLists(t *testing.T) {
	cat := Filters{Field: "title", FilterType: "contains", Value: "cat"}
	dog := Filters{Field: "title", FilterType: "contains", Value: "dog"}
	filteredMeta := []FilteredMetaOps{
		{Filters: []Filters{cat, dog}, MetaOps: []MetaOps{{Field: "artist", OpType: "set", OpValue: "A"}, {Field: "album", OpType: "set", OpValue: "B"}}},
		{Filters: []Filters{dog}, MetaOps: []MetaOps{{Field: "artist", OpType: "set", OpValue: "C"}}},
	}
	filteredFilename := []FilteredFilenameOps{
		{Filters: []Filters{cat, dog}, FilenameOps: []FilenameOps{{OpType: "prefix", OpValue: "[A] "}, {OpType: "append", OpValue: " [B]"}}},
		{Filters: []Filters{dog}, FilenameOps: []FilenameOps{{OpType: "prefix", OpValue: "[C] "}}},
	}
	parent := &MetarrArgs{FilteredMetaOps: filteredMeta, FilteredFilenameOps: filteredFilename}
	child := &MetarrArgs{
		FilteredMetaOps:     append([]FilteredMetaOps(nil), filteredMeta...),
		FilteredFilenameOps: append([]FilteredFilenameOps(nil), filteredFilename...),
	}
	if !ChildMetarrArgsMatchParent(parent, child) {
		t.Error("identical filtered ops reported as not matching the parent")
	}

	child.FilteredMetaOps = filteredMeta[:1]
	if ChildMetarrArgsMatchParent(parent, child) {
		t.Error("fewer filtered meta ops reported as matching the parent")
	}
}

// TestChildMatchParentNil tests that nil models compare without panicking.
func TestChildMatchParentNil(t *testing.T) {
	if !ChildSettingsMatchParent(nil, nil) {
		t.Error("nil Settings should match nil")
	}
	if ChildSettingsMatchParent(nil, &Settings{}) || ChildSettingsMatchParent(&Settings{}, nil) {
		t.Error("nil Settings should only match nil")
	}
	if !ChildMetarrArgsMatchParent(nil, nil) {
		t.Error("nil MetarrArgs should match nil")
	}
	if ChildMetarrArgsMatchParent(nil, &MetarrArgs{}) || ChildMetarrArgsMatchParent(&MetarrArgs{}, nil) {
		t.Error("nil MetarrArgs should only match nil")
	}
}
