package models

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
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
		{Filters: filters[:1], OutputDir: "/cats"},
		{Filters: filters, OutputDir: "/dogs"},
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
		{Filters: []Filters{cat, dog}, FilenameOps: []FilenameOps{{OpType: "prefix", OpValue: "[A] "}, {OpType: "suffix", OpValue: " [B]"}}},
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

// TestFilterConflictingMetaOps tests base ops are dropped only when an override op of a conflicting type has their field.
func TestFilterConflictingMetaOps(t *testing.T) {
	base := []MetaOps{
		{Field: "artist", OpType: "set", OpValue: "Base"},
		{Field: "album", OpType: "set", OpValue: "Base"},
		{Field: "title", OpType: "suffix", OpValue: " [Base]"},
	}
	overrides := []MetaOps{
		{Field: "artist", OpType: "set", OpValue: "Override"},
		{Field: "title", OpType: "suffix", OpValue: " [Override]"},
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

// A metarr_args blob as the old version would have written it for:
//
//	title:date-tag:prefix:ymd
//	fulltitle:date-tag:prefix:ymd
//	all-credits:set:Kino Casino
const oldBlob = `{
  "metarr_output_ext": "mp4",
  "metarr_rename_style": "spaces",
  "metarr_meta_ops": [
    {"meta_op_channel_url":"","meta_op_field":"title","meta_op_find_string":"","meta_op_type":"date-tag","meta_op_value":"","meta_op_loc":"prefix","meta_op_date_format":"ymd"},
    {"meta_op_channel_url":"","meta_op_field":"fulltitle","meta_op_find_string":"","meta_op_type":"date-tag","meta_op_value":"","meta_op_loc":"prefix","meta_op_date_format":"ymd"},
    {"meta_op_channel_url":"","meta_op_field":"all-credits","meta_op_find_string":"","meta_op_type":"set","meta_op_value":"Kino Casino","meta_op_loc":"","meta_op_date_format":""}
  ],
  "metarr_filename_ops": [
    {"filename_op_channel_url":"","filename_op_type":"date-tag","filename_op_find_string":"","filename_op_value":"","filename_op_loc":"prefix","filename_op_date_format":"ymd"}
  ],
  "metarr_filtered_meta_ops": [
    {"Filters":[{"filter_url_specific":"","filter_field":"title","filter_type":"contains","filter_value":"cat","filter_must_any":"must"}],
     "MetaOps":[{"meta_op_channel_url":"","meta_op_field":"title","meta_op_find_string":"","meta_op_type":"prefix","meta_op_value":"[CAT] ","meta_op_loc":"","meta_op_date_format":""}],
     "FiltersMatched":false}
  ]
}`

func TestOldDBBlobStillReads(t *testing.T) {
	var args MetarrArgs
	if err := json.Unmarshal([]byte(oldBlob), &args); err != nil {
		t.Fatalf("failed to read an old metarr_args blob: %v", err)
	}

	if len(args.MetaOps) != 3 {
		t.Fatalf("read %d meta ops, want 3", len(args.MetaOps))
	}
	if args.MetaOps[0].Field != "title" || args.MetaOps[0].OpType != "date-tag" ||
		args.MetaOps[0].OpLoc != "prefix" || args.MetaOps[0].DateFormat != "ymd" {
		t.Errorf("title date-tag did not survive: %+v", args.MetaOps[0])
	}
	if args.MetaOps[1].Field != "fulltitle" {
		t.Errorf("fulltitle date-tag did not survive: %+v", args.MetaOps[1])
	}
	if args.MetaOps[2].Field != "all-credits" || args.MetaOps[2].OpValue != "Kino Casino" {
		t.Errorf("all-credits set did not survive: %+v", args.MetaOps[2])
	}

	if len(args.FilenameOps) != 1 || args.FilenameOps[0].DateFormat != "ymd" {
		t.Errorf("filename ops did not survive: %+v", args.FilenameOps)
	}

	if len(args.FilteredMetaOps) != 1 {
		t.Fatalf("read %d filtered entries, want 1", len(args.FilteredMetaOps))
	}
	fmo := args.FilteredMetaOps[0]
	if len(fmo.Filters) != 1 || fmo.Filters[0].Field != "title" || fmo.Filters[0].MustAny != "must" {
		t.Errorf("filters did not survive: %+v", fmo.Filters)
	}
	if len(fmo.MetaOps) != 1 || fmo.MetaOps[0].OpValue != "[CAT] " {
		t.Errorf("filtered meta ops did not survive: %+v", fmo.MetaOps)
	}

	// Re-marshal and confirm the operation arrays are written back in the same shape, so
	// saving a channel after reading it does not rewrite existing rows into a new format.
	// Only the channel URL keys, dropped with channel URL prefixes, are left out.
	out, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}

	withoutChannelURLs := regexp.MustCompile(`"(meta_op_channel_url|filename_op_channel_url|filter_url_specific)":"",\s*`).ReplaceAllString(oldBlob, "")
	var before, after map[string]json.RawMessage
	if err := json.Unmarshal([]byte(withoutChannelURLs), &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &after); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{"metarr_meta_ops", "metarr_filename_ops", "metarr_filtered_meta_ops"} {
		if !jsonEqual(t, before[key], after[key]) {
			t.Errorf("%s changed shape:\n old %s\n new %s", key, before[key], after[key])
		}
	}
}

// jsonEqual compares two JSON documents ignoring key order.
func jsonEqual(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()

	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("unmarshal %s: %v", a, err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}

	an, _ := json.Marshal(av)
	bn, _ := json.Marshal(bv)
	return string(an) == string(bn)
}
