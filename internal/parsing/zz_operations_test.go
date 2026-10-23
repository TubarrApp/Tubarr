package parsing_test

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedparsing"
)

// TestMain gives the global logger a console writer, since the zero value has none and
// logging on an error path would otherwise dereference nil.
func TestMain(m *testing.M) {
	logger.Pl.Console = io.Discard
	os.Exit(m.Run())
}

// TestMetaOpsRoundTripThroughTubarr checks the pipeline Tubarr actually runs: parse and
// validate user input, format it for Metarr's argv, and confirm Metarr would parse back
// the identical operation.
func TestMetaOpsRoundTripThroughTubarr(t *testing.T) {
	in := []string{
		`title:set:cats`,
		`title:replace:old:new`,
		`title:date-tag:prefix:md`,
		`title:set:cats\: the sequel`,
		`description:append:back\\slash`,
		`title:prefix:[CATS] `,
	}

	parsed, err := parsing.ParseMetaOps(in)
	if err != nil {
		t.Fatalf("ParseMetaOps: %v", err)
	}
	if len(parsed) != len(in) {
		t.Fatalf("parsed %d ops, want %d: %+v", len(parsed), len(in), parsed)
	}

	formatted := make([]string, 0, len(parsed))
	for _, op := range parsed {
		formatted = append(formatted, sharedparsing.FormatMetaOp(op, true))
	}

	again, err := parsing.ParseMetaOps(formatted)
	if err != nil {
		t.Fatalf("re-parse of %v: %v", formatted, err)
	}
	for i := range parsed {
		if parsed[i] != again[i] {
			t.Errorf("operation %d changed across the handoff:\n first %+v\n again %+v\n via   %q", i, parsed[i], again[i], formatted[i])
		}
	}
}

// TestTwoCharDateFormatAccepted guards the fix for "md" and "dm", which Tubarr's own
// validator used to reject even though Metarr accepted them.
func TestTwoCharDateFormatAccepted(t *testing.T) {
	for _, fmtStr := range []string{"md", "dm"} {
		if !validation.ValidateDateFormat(fmtStr) {
			t.Errorf("date format %q must be accepted", fmtStr)
		}
	}
}

// TestFilenameOpsRoundTripThroughTubarr mirrors the meta op pipeline for filenames.
func TestFilenameOpsRoundTripThroughTubarr(t *testing.T) {
	in := []string{
		`prefix:[DOG VIDEOS]`,
		`date-tag:prefix:ymd`,
		`delete-date-tag:all:dm`,
		`replace-suffix:_1:`,
		`prefix:[CATS\: THE SEQUEL] `,
	}

	parsed, err := parsing.ParseFilenameOps(in)
	if err != nil {
		t.Fatalf("ParseFilenameOps: %v", err)
	}

	formatted := make([]string, 0, len(parsed))
	for _, op := range parsed {
		formatted = append(formatted, sharedparsing.FormatFilenameOp(op, true))
	}

	again, err := parsing.ParseFilenameOps(formatted)
	if err != nil {
		t.Fatalf("re-parse of %v: %v", formatted, err)
	}
	for i := range parsed {
		if parsed[i] != again[i] {
			t.Errorf("operation %d changed across the handoff:\n first %+v\n again %+v\n via   %q", i, parsed[i], again[i], formatted[i])
		}
	}
}

// TestUnescapedPipeInValue covers titles such as "Cats | Dogs". The segment before the
// pipe parses as a URI with a scheme, so requiring a host is what stops it being taken
// for a channel URL prefix and the operation being discarded.
func TestUnescapedPipeInValue(t *testing.T) {
	parsed, err := parsing.ParseMetaOps([]string{"title:set:Cats | Dogs"})
	if err != nil {
		t.Fatalf("ParseMetaOps: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("parsed %d ops, want 1: %+v", len(parsed), parsed)
	}
	if parsed[0].ChannelURL != "" {
		t.Errorf("ChannelURL = %q, want empty", parsed[0].ChannelURL)
	}
	if parsed[0].OpValue != "Cats | Dogs" {
		t.Errorf("OpValue = %q, want %q", parsed[0].OpValue, "Cats | Dogs")
	}

	// It must also survive the handoff to Metarr.
	formatted := sharedparsing.FormatMetaOp(parsed[0], false)
	again, err := parsing.ParseMetaOps([]string{formatted})
	if err != nil {
		t.Fatalf("re-parse of %q: %v", formatted, err)
	}
	if again[0] != parsed[0] {
		t.Errorf("round trip changed the op:\n via  %q\n want %+v\n got  %+v", formatted, parsed[0], again[0])
	}
}

// TestChannelURLRejected tests that every op and filter parser rejects an entry scoped to a channel URL.
func TestChannelURLRejected(t *testing.T) {
	const u = "https://example.com/@cats"
	parsers := map[string]func(string) error{
		"meta": func(s string) error {
			_, err := parsing.ParseMetaOps([]string{s})
			return err
		},
		"filename": func(s string) error {
			_, err := parsing.ParseFilenameOps([]string{s})
			return err
		},
		"filter": func(s string) error {
			_, err := parsing.ParseFilterOps([]string{s}, true)
			return err
		},
		"filtered meta": func(s string) error {
			_, err := parsing.ParseFilteredMetaOps([]string{s})
			return err
		},
		"filtered filename": func(s string) error {
			_, err := parsing.ParseFilteredFilenameOps([]string{s})
			return err
		},
	}
	entries := map[string][]string{
		"meta":              {u + "|title:set:Cats", u + "||title:set:Cats"},
		"filename":          {u + "|prefix:[CATS] ", u + "||prefix:[CATS] "},
		"filter":            {u + "|title:contains:cat:must", u + "||title:contains:cat:must"},
		"filtered meta":     {u + "|title:contains:cat||genre:set:Cats", "title:contains:cat||" + u + "|genre:set:Cats", u + "||title:contains:cat||genre:set:Cats"},
		"filtered filename": {u + "|title:contains:cat||prefix:[CATS] ", "title:contains:cat||" + u + "|prefix:[CATS] "},
	}
	for name, parse := range parsers {
		for _, entry := range entries[name] {
			if err := parse(entry); err == nil {
				t.Errorf("%s parser accepted %q", name, entry)
			}
		}
	}
}

// TestFilteredOpsWithoutChannelURL keeps the unscoped form working, which must not be
// mistaken for a channel URL prefix.
func TestFilteredOpsWithoutChannelURL(t *testing.T) {
	got, err := parsing.ParseFilteredMetaOps([]string{"title:contains:cat||director:set:Mr Cat"})
	if err != nil {
		t.Fatalf("ParseFilteredMetaOps: %v", err)
	}
	if len(got) != 1 || len(got[0].Filters) != 1 || len(got[0].MetaOps) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].Filters[0].ChannelURL != "" || got[0].MetaOps[0].ChannelURL != "" {
		t.Errorf("no channel URL expected, got filter=%q op=%q", got[0].Filters[0].ChannelURL, got[0].MetaOps[0].ChannelURL)
	}
}

// TestParseFilterOps tests the ParseFilterOps function for both must/any and non-must/any filters.
func TestParseFilterOps(t *testing.T) {
	// Must/Any filter test.
	mustAnyFilterOps := []string{
		"title:contains:cat:must",
		"title:omits:frogs:any",
		"date:omits:must",
		"duration:morethan:3600:must",
	}
	invalidMustAnyFilterOps := []string{
		"title:contains:cat",
		"title:omits:frogs",
		"date:omits",
		"duration:morethan:3600",
	}

	mustAnyFilters, err := parsing.ParseFilterOps(mustAnyFilterOps, true)
	if err != nil {
		t.Fatalf("ParseFilterOps returned an error: %v", err)
	}
	if _, err := parsing.ParseFilterOps(invalidMustAnyFilterOps, true); err == nil {
		t.Fatalf("ParseFilterOps should have returned an error for invalid must/any filters")
	}

	if len(mustAnyFilters) != len(mustAnyFilterOps) {
		t.Fatalf("Expected %d filters, got %d", len(mustAnyFilterOps), len(mustAnyFilters))
	}

	for i, mustAnyFilter := range mustAnyFilters {
		var actual string

		expected := mustAnyFilterOps[i]
		split := strings.Split(expected, ":")

		if len(split) == 4 {
			actual = fmt.Sprintf("%s:%s:%s:%s", mustAnyFilter.Field, mustAnyFilter.FilterType, mustAnyFilter.Value, mustAnyFilter.MustAny)
		} else {
			actual = fmt.Sprintf("%s:%s:%s", mustAnyFilter.Field, mustAnyFilter.FilterType, mustAnyFilter.MustAny)
		}
		if expected != actual {
			t.Errorf("Expected filter %q, got %q", expected, actual)
		}
	}

	// Non-Must/Any filter test.
	nonMustAnyFilterOps := []string{
		"title:contains:cat",
		"title:omits:frogs",
		"date:omits",
		"duration:morethan:3600",
	}
	invalidNonMustAnyFilterOps := []string{
		"title:contains:cat:must",
		"title:omits:frogs:any",
		"date:omits:must",
		"duration:morethan:3600:any",
	}

	noMustAnyFilters, err := parsing.ParseFilterOps(nonMustAnyFilterOps, false)
	if err != nil {
		t.Fatalf("ParseFilterOps returned an error: %v", err)
	}
	if _, err := parsing.ParseFilterOps(invalidNonMustAnyFilterOps, false); err == nil {
		t.Fatalf("ParseFilterOps should have returned an error for invalid non-must/any filters")
	}

	if len(noMustAnyFilters) != len(nonMustAnyFilterOps) {
		t.Fatalf("Expected %d filters, got %d", len(nonMustAnyFilterOps), len(noMustAnyFilters))
	}

	for i, noMustAnyFilter := range noMustAnyFilters {
		var actual string

		expected := nonMustAnyFilterOps[i]
		split := strings.Split(expected, ":")

		if len(split) == 3 {
			actual = fmt.Sprintf("%s:%s:%s", noMustAnyFilter.Field, noMustAnyFilter.FilterType, noMustAnyFilter.Value)
		} else {
			actual = fmt.Sprintf("%s:%s", noMustAnyFilter.Field, noMustAnyFilter.FilterType)
		}
		if expected != actual {
			t.Errorf("Expected filter %q, got %q", expected, actual)
		}
	}

}

// TestParseMetaFilterMoveOps tests the filter|directory form, and that it's written back the same way.
func TestParseMetaFilterMoveOps(t *testing.T) {
	tests := []struct {
		in   string
		want models.MetaFilterMoveOps
	}{
		{"Title:contains:Cats||/home/cats", models.MetaFilterMoveOps{Field: "title", FilterType: "contains", Value: "cats", OutputDir: "/home/cats"}},
		{"title:contains:Ep 5 | Show: Finale||/home/show", models.MetaFilterMoveOps{Field: "title", FilterType: "contains", Value: "ep 5 | show: finale", OutputDir: "/home/show"}},
		{"title:omits:dogs||/home/no dogs", models.MetaFilterMoveOps{Field: "title", FilterType: "omits", Value: "dogs", OutputDir: "/home/no dogs"}},
		{"duration:morethan:3600||/home/long", models.MetaFilterMoveOps{Field: "duration", FilterType: "morethan", Value: "3600", OutputDir: "/home/long"}},
		{"director:contains||/home/directed", models.MetaFilterMoveOps{Field: "director", FilterType: "contains", OutputDir: "/home/directed"}},
		{`title:contains:cats||C:\Videos\Cats`, models.MetaFilterMoveOps{Field: "title", FilterType: "contains", Value: "cats", OutputDir: `C:\Videos\Cats`}},
		{"title:contains:cats||/home/a|b||c", models.MetaFilterMoveOps{Field: "title", FilterType: "contains", Value: "cats", OutputDir: "/home/a|b||c"}},
		{`title:contains:ends in a pipe\|||/home/pipe`, models.MetaFilterMoveOps{Field: "title", FilterType: "contains", Value: "ends in a pipe|", OutputDir: "/home/pipe"}},
	}
	for _, tt := range tests {
		got, err := parsing.ParseMetaFilterMoveOps([]string{tt.in})
		if err != nil || len(got) != 1 || got[0] != tt.want {
			t.Errorf("ParseMetaFilterMoveOps(%q) = %+v, %v; want %+v", tt.in, got, err, tt.want)
			continue
		}
		// The written form reads back the same.
		line := models.MetaFilterMoveOpsToString(got[0])
		if again, err := parsing.ParseMetaFilterMoveOps([]string{line}); err != nil || len(again) != 1 || again[0] != tt.want {
			t.Errorf("%q written as %q read back as %+v, %v", tt.in, line, again, err)
		}
	}

	for _, bad := range []string{
		"title:contains:cats",       // No directory.
		"title:contains:cats||",     // Empty directory.
		"title:contains:cats|/home", // A single '|'.
		"title:morethan||/home",     // A comparison with no value.
		"https://tubesite.com/@cats|title:contains:cats||/home/cats",  // A channel URL.
		"https://tubesite.com/@cats||title:contains:cats||/home/cats", // A channel URL with "||".
	} {
		if _, err := parsing.ParseMetaFilterMoveOps([]string{bad}); err == nil {
			t.Errorf("ParseMetaFilterMoveOps(%q) was accepted", bad)
		}
	}
	for _, invalid := range []string{"title:morethan:soon||/home", "title:explodes:cats||/home"} {
		ops, err := parsing.ParseMetaFilterMoveOps([]string{invalid})
		if err == nil {
			err = validation.ValidateMetaFilterMoveOps(ops, false)
		}
		if err == nil {
			t.Errorf("%q was accepted", invalid)
		}
	}
}

// TestMoveOpsOldBlob tests move ops saved before filter types still read, as contains filters.
func TestMoveOpsOldBlob(t *testing.T) {
	var s models.Settings
	if err := json.Unmarshal([]byte(`{"move_ops":[{"move_url_specific":"","move_op_field":"title","move_op_value":"cats","move_op_output_dir":"/cats"}]}`), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.MetaFilterMoveOps) != 1 {
		t.Fatalf("got %d move ops, want 1", len(s.MetaFilterMoveOps))
	}
	if f := s.MetaFilterMoveOps[0].Filter(); f.FilterType != "contains" || f.Field != "title" || f.Value != "cats" {
		t.Errorf("old move op's filter = %+v, want title contains cats", f)
	}
	if got := models.MetaFilterMoveOpsToString(s.MetaFilterMoveOps[0]); got != "title:contains:cats||/cats" {
		t.Errorf("old move op written as %q, want the new form", got)
	}
}

// TestParseNotificationsBareURL tests a notification can be a bare URL, and must be http(s).
func TestParseNotificationsBareURL(t *testing.T) {
	got, err := parsing.ParseNotifications([]string{"http://jellyfin.local/Library/Refresh"})
	if err != nil || len(got) != 1 || got[0].NotifyURL != "http://jellyfin.local/Library/Refresh" || got[0].ChannelURL != "" {
		t.Errorf("bare URL = %+v, %v", got, err)
	}
	if _, err := parsing.ParseNotifications([]string{"Plex Library"}); err == nil {
		t.Error("a name alone was accepted as a notification URL")
	}
	if _, err := parsing.ParseNotifications([]string{"https://localhost:32400/library/sections/< YOUR LIBRARY NUMBER >/refresh"}); err == nil {
		t.Error("a URL containing spaces and angle brackets was accepted")
	}
}

// TestParseNotificationsChannelScoped tests "Channel URL|Notify URL" scopes a notification, and that both must be http(s) URLs.
func TestParseNotificationsChannelScoped(t *testing.T) {
	got, err := parsing.ParseNotifications([]string{"https://tubesite.com/@chan|http://plex:32400/refresh?X-Plex-Token=a"})
	if err != nil || len(got) != 1 || got[0].ChannelURL != "https://tubesite.com/@chan" || got[0].NotifyURL != "http://plex:32400/refresh?X-Plex-Token=a" {
		t.Errorf("channel scoped = %+v, %v", got, err)
	}
	for _, bad := range []string{
		"http://plex:32400/refresh|Plex",                            // Old "Notify URL|Name" form.
		"https://tubesite.com/@chan|http://plex:32400/refresh|Plex", // Old "Channel URL|Notify URL|Name" form.
		"tubesite|http://plex:32400/refresh",                        // Channel URL isn't a URL.
	} {
		if _, err := parsing.ParseNotifications([]string{bad}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
