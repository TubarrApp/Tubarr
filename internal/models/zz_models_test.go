package models

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"tubarr/internal/domain/logger"
)

// ******************************** Channels ******************************************************************************************

// TestDueURLs tests URLs are due on their own crawl frequency, or the channel's, and that the web scheduler uses the
// default for an unset frequency rather than every check.
func TestDueURLs(t *testing.T) {
	now := time.Now()
	ago := func(minutes int) time.Time { return now.Add(-time.Duration(minutes) * time.Minute) }
	names := func(cus []*ChannelURL) []string {
		var out []string
		for _, cu := range cus {
			out = append(out, cu.URL)
		}
		return out
	}

	c := &Channel{
		ChanSettings: &Settings{CrawlFreq: 60},
		URLModels: []*ChannelURL{
			{URL: "own-due", LastScan: ago(20), ChanURLSettings: &Settings{CrawlFreq: 15}},
			{URL: "own-waiting", LastScan: ago(20), ChanURLSettings: &Settings{CrawlFreq: 45}},
			{URL: "channel-waiting", LastScan: ago(20), ChanURLSettings: &Settings{}},
			{URL: "channel-due", LastScan: ago(61)},
			{URL: "paused", LastScan: ago(600), ChanURLSettings: &Settings{Paused: true}},
			{URL: "manual", LastScan: ago(600), IsManual: true},
		},
	}
	due, next := c.DueURLs(now, 0, true)
	if got := names(due); !slices.Equal(got, []string{"own-due", "channel-due"}) {
		t.Errorf("due URLs = %v, want own-due and channel-due", got)
	}
	if next != 25*time.Minute {
		t.Errorf("next due in %s, want 25m (own-waiting)", next)
	}
	if due, _ := c.DueURLs(now, 10*time.Minute, true); len(due) != 0 {
		t.Errorf("with 10 minutes of jitter, due URLs = %v, want none", names(due))
	}

	unset := &Channel{ChanSettings: &Settings{CrawlFreq: 0}, URLModels: []*ChannelURL{{URL: "u", LastScan: ago(5)}}}
	if due, next := unset.DueURLs(now, 0, true); len(due) != 0 || next != time.Duration(DefaultCrawlFreq-5)*time.Minute {
		t.Errorf("scheduled with no frequency: due %v, next %s; want none, by the default frequency", names(due), next)
	}
	if due, _ := unset.DueURLs(now, 0, false); len(due) != 1 {
		t.Error("terminal run with no frequency didn't crawl the URL")
	}
}

// ******************************** FlareSolverr **************************************************************************************

// TestFlareSolverrSolutionRefresh tests that concurrent refreshes after the same stale generation solve only once.
func TestFlareSolverrSolutionRefresh(t *testing.T) {
	var (
		mu     sync.Mutex
		solves int
	)
	s := NewFlareSolverrSolution(FlareSolverrSolve{UserAgent: "UA/1", Gen: 1}, func(staleGen int) (FlareSolverrSolve, error) {
		mu.Lock()
		defer mu.Unlock()
		solves++
		return FlareSolverrSolve{UserAgent: "UA/2", Gen: staleGen + 1}, nil
	}, nil)

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if err := s.Refresh(1); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()

	if solves != 1 {
		t.Errorf("expected 1 solve for 5 refreshes of the same generation, got %d", solves)
	}
	if solve := s.Current(); solve.UserAgent != "UA/2" || solve.Gen != 2 {
		t.Errorf("expected the refreshed user agent and generation, got %+v", solve)
	}
}

// TestIsCloudflareBlock tests recognising yt-dlp errors caused by Cloudflare.
func TestIsCloudflareBlock(t *testing.T) {
	blocked := []string{
		"ERROR: [generic] Unable to download webpage: HTTP Error 403: Forbidden",
		"ERROR: Got HTTP Error 403 caused by Cloudflare anti-bot challenge",
		"<title>Just a moment...</title>",
	}
	for _, out := range blocked {
		if !IsCloudflareBlock(out) {
			t.Errorf("expected %q to be a Cloudflare block", out)
		}
	}
	if IsCloudflareBlock("ERROR: Unsupported URL: https://example.com") {
		t.Error("expected an unsupported URL error not to be a Cloudflare block")
	}
}

// TestFlareSolverrSolutionCurrentNewer tests that Current switches to a newer solve made meanwhile, and keeps its own otherwise.
func TestFlareSolverrSolutionCurrentNewer(t *testing.T) {
	latest := FlareSolverrSolve{UserAgent: "UA/1", Gen: 1}
	s := NewFlareSolverrSolution(latest, nil, func(gen int) (FlareSolverrSolve, bool, error) {
		return latest, latest.Gen > gen, nil
	})

	if got := s.Current(); got.Gen != 1 {
		t.Errorf("expected solve 1, got %+v", got)
	}
	latest = FlareSolverrSolve{UserAgent: "UA/2", Gen: 2}
	if got := s.Current(); got.Gen != 2 || got.UserAgent != "UA/2" {
		t.Errorf("expected to switch to the newer solve 2, got %+v", got)
	}
}

// ******************************** Levels ********************************************************************************************

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

// ******************************** Settings ******************************************************************************************

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
			if strings.HasSuffix(name, "ReplaceInherited") {
				continue // Tested in TestReplaceTicksNeverMatch.
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

// TestReplaceTicksNeverMatch tests that a child's replace tick is its own, so a ticked child never matches a parent
// ticked too, and a tick alone isn't all zero.
func TestReplaceTicksNeverMatch(t *testing.T) {
	for _, tc := range []struct {
		model   any
		match   func(parent, child any) bool
		allZero func(child any) bool
	}{
		{Settings{}, func(p, c any) bool { return ChildSettingsMatchParent(p.(*Settings), c.(*Settings)) }, func(c any) bool { return SettingsAllZero(c.(*Settings)) }},
		{MetarrArgs{}, func(p, c any) bool { return ChildMetarrArgsMatchParent(p.(*MetarrArgs), c.(*MetarrArgs)) }, func(c any) bool { return MetarrArgsAllZero(c.(*MetarrArgs)) }},
	} {
		typ := reflect.TypeOf(tc.model)
		for i := range typ.NumField() {
			name := typ.Name() + "." + typ.Field(i).Name
			if !strings.HasSuffix(name, "ReplaceInherited") {
				continue
			}
			parent, child := reflect.New(typ), reflect.New(typ)
			parent.Elem().Field(i).SetBool(true)
			child.Elem().Field(i).SetBool(true)
			if tc.match(parent.Interface(), child.Interface()) {
				t.Errorf("%s: a ticked child matches its parent, so it isn't saved", name)
			}
			if tc.allZero(child.Interface()) {
				t.Errorf("%s: a tick alone counts as all zero, so it isn't saved", name)
			}
		}
	}
}

// TestMetarrArgsAllZeroEachField tests that any one Metarr arg set makes MetarrArgs non-zero.
func TestMetarrArgsAllZeroEachField(t *testing.T) {
	typ := reflect.TypeFor[MetarrArgs]()
	for i := range typ.NumField() {
		m := reflect.New(typ)
		setNonZero(m.Elem().Field(i), 1)
		if MetarrArgsAllZero(m.Interface().(*MetarrArgs)) {
			t.Errorf("MetarrArgs.%s set still counts as all zero", typ.Field(i).Name)
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

// ******************************** Video JSON ****************************************************************************************

// TestVideoJSONFlat tests that a video's metadata and file fields are written at the top level, and read back.
func TestVideoJSONFlat(t *testing.T) {
	v := Video{
		ID:          5,
		ChannelName: "Chan",
		Metadata:    VideoMetadata{Title: "T", Duration: 60, Raw: map[string]any{"x": 1}},
		Files:       VideoFiles{VideoFilePath: "/v.mp4", JSONFilePath: "/v.json", VideoDir: "/dir"},
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var flat map[string]any
	if err := json.Unmarshal(data, &flat); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"id": 5.0, "channel_name": "Chan", "title": "T", "duration": 60.0, "video_path": "/v.mp4", "json_path": "/v.json"} {
		if flat[key] != want {
			t.Errorf("%s = %v, want %v in %s", key, flat[key], want, data)
		}
	}
	for _, key := range []string{"Metadata", "Files", "Raw", "VideoDir"} {
		if _, ok := flat[key]; ok {
			t.Errorf("%s written in %s", key, data)
		}
	}

	var got Video
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 5 || got.ChannelName != "Chan" || got.Metadata.Title != "T" || got.Metadata.Duration != 60 || got.Files.VideoFilePath != "/v.mp4" || got.Files.JSONFilePath != "/v.json" {
		t.Errorf("read back %+v", got)
	}
}

// TestAvailableVideoJSON tests that an available video keeps its crawl retries alongside the flat video fields.
func TestAvailableVideoJSON(t *testing.T) {
	v := AvailableVideo{
		Video:        Video{ID: 7, Metadata: VideoMetadata{Title: "T"}},
		CrawlRetries: CrawlRetries{RetriesOnCrawl: true, CrawlDLRetries: 2, CrawlDLRetriesLeft: 1},
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var flat map[string]any
	if err := json.Unmarshal(data, &flat); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]any{"id": 7.0, "title": "T", "retries_on_crawl": true, "crawl_dl_retries_left": 1.0} {
		if flat[key] != want {
			t.Errorf("%s = %v, want %v in %s", key, flat[key], want, data)
		}
	}

	var got AvailableVideo
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != 7 || got.Metadata.Title != "T" || !got.RetriesOnCrawl || got.CrawlDLRetriesLeft != 1 || got.CrawlDLRetries != 0 {
		t.Errorf("read back %+v", got)
	}
}

// ******************************** Videos ********************************************************************************************

// TestStoreFilenamesFromMetarr tests that the last reported video path wins, so a path reported mid-run survives a
// failure before the final one.
func TestStoreFilenamesFromMetarr(t *testing.T) {
	logger.Pl.Console = io.Discard

	tests := []struct {
		name, out, wantVideo, wantJSON string
		wantErr                        bool
	}{
		{"final", "final video path: /out/v.mp4\nfinal json path: /out/v.json\n", "/out/v.mp4", "/out/v.json", false},
		{"current then final", "current video path: /in/v.mp4\nfinal video path: /out/v.mp4\nfinal json path: /out/v.json\n", "/out/v.mp4", "/out/v.json", false},
		{"current only", "current video path: /in/v.mp4\n", "/in/v.mp4", "/in/v.json", false},
		{"metafile purged", "final video path: /out/v.mp4\nfinal json path: \n", "/out/v.mp4", "", false},
		{"none", "some other output\n", "/in/v.webm", "/in/v.json", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := &Video{Files: VideoFiles{VideoFilePath: "/in/v.webm", JSONFilePath: "/in/v.json"}}
			err := v.StoreFilenamesFromMetarr(tt.out)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if v.Files.VideoFilePath != tt.wantVideo || v.Files.JSONFilePath != tt.wantJSON {
				t.Errorf("paths = %q, %q; want %q, %q", v.Files.VideoFilePath, v.Files.JSONFilePath, tt.wantVideo, tt.wantJSON)
			}
		})
	}
}
