package parsing_test

import (
	"io"
	"os"
	"testing"
	"tubarr/internal/domain/logger"
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
