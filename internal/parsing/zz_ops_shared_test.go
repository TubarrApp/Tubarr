package parsing_test

import (
	"io"
	"os"
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

// TestMetaOpsRoundTripThroughTubarr checks the pipeline Tubarr actually runs: parse
// user input, validate it, format it for Metarr's argv, and confirm Metarr would parse
// back the identical operation.
func TestMetaOpsRoundTripThroughTubarr(t *testing.T) {
	in := []string{
		`title:set:cats`,
		`title:replace:old:new`,
		`title:date-tag:prefix:md`,
		`title:set:cats\: the sequel`,
		`description:append:back\\slash`,
		`https://example.com|title:prefix:[CATS] `,
	}

	parsed, err := parsing.ParseMetaOps(in)
	if err != nil {
		t.Fatalf("ParseMetaOps: %v", err)
	}
	if len(parsed) != len(in) {
		t.Fatalf("parsed %d ops, want %d: %+v", len(parsed), len(in), parsed)
	}
	if err := validation.ValidateMetaOps(parsed); err != nil {
		t.Fatalf("ValidateMetaOps: %v", err)
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
	if err := validation.ValidateFilenameOps(parsed); err != nil {
		t.Fatalf("ValidateFilenameOps: %v", err)
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

// TestFilteredOpsRoundTripWithChannelURL covers a filtered entry scoped to a channel.
// Tubarr's own serializer emits the "channelURL|filter|ops" form, so failing to parse
// it back meant a channel-scoped filtered operation could be saved but never read.
func TestFilteredOpsRoundTripWithChannelURL(t *testing.T) {
	const chanURL = "https://example.com"

	meta := models.FilteredMetaOps{
		Filters: []models.Filters{{ChannelURL: chanURL, Field: "title", FilterType: "contains", Value: "cat"}},
		MetaOps: []models.MetaOps{{Field: "director", OpType: "set", OpValue: "Mr Cat"}},
	}

	serialized := models.FilteredMetaOpsToSlice(meta)
	if len(serialized) != 1 {
		t.Fatalf("serialized to %q, want one entry", serialized)
	}

	got, err := parsing.ParseFilteredMetaOps(serialized)
	if err != nil {
		t.Fatalf("ParseFilteredMetaOps(%q): %v", serialized, err)
	}
	if len(got) != 1 {
		t.Fatalf("parsed %d entries, want 1", len(got))
	}
	if len(got[0].Filters) != 1 || got[0].Filters[0].Field != "title" || got[0].Filters[0].Value != "cat" {
		t.Errorf("filters = %+v", got[0].Filters)
	}
	if len(got[0].MetaOps) != 1 || got[0].MetaOps[0].Field != "director" || got[0].MetaOps[0].OpValue != "Mr Cat" {
		t.Errorf("meta ops = %+v", got[0].MetaOps)
	}
	if got[0].Filters[0].ChannelURL != chanURL || got[0].MetaOps[0].ChannelURL != chanURL {
		t.Errorf("channel URL not applied: filter=%q op=%q", got[0].Filters[0].ChannelURL, got[0].MetaOps[0].ChannelURL)
	}
}

// TestFilteredOpsWithoutChannelURL keeps the unscoped form working, which must not be
// mistaken for a channel URL prefix.
func TestFilteredOpsWithoutChannelURL(t *testing.T) {
	got, err := parsing.ParseFilteredMetaOps([]string{"title:contains:cat|director:set:Mr Cat"})
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

// TestFilteredFilenameOpsRoundTripWithChannelURL mirrors the meta case for filenames.
func TestFilteredFilenameOpsRoundTripWithChannelURL(t *testing.T) {
	const chanURL = "https://example.com"

	f := models.FilteredFilenameOps{
		Filters:     []models.Filters{{ChannelURL: chanURL, Field: "title", FilterType: "contains", Value: "cat"}},
		FilenameOps: []models.FilenameOps{{OpType: "prefix", OpValue: "[CATS] "}},
	}

	serialized := models.FilteredFilenameOpsToSlice(f)
	got, err := parsing.ParseFilteredFilenameOps(serialized)
	if err != nil {
		t.Fatalf("ParseFilteredFilenameOps(%q): %v", serialized, err)
	}
	if len(got) != 1 || len(got[0].FilenameOps) != 1 {
		t.Fatalf("got %+v", got)
	}
	if got[0].FilenameOps[0].OpValue != "[CATS] " {
		t.Errorf("filename op = %+v", got[0].FilenameOps[0])
	}
	if got[0].FilenameOps[0].ChannelURL != chanURL {
		t.Errorf("channel URL not applied, got %q", got[0].FilenameOps[0].ChannelURL)
	}
}
