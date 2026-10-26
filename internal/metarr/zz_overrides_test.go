package metarr

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/TubarrApp/gocommon/sharedparsing"
)

const (
	ownURL   = "https://example.com/@a"
	otherURL = "https://example.com/@b"
)

// TestChannelURLOverrideLines tests the Download As dialog gets the channel URL's directories and ops, and that its
// lines parse back.
func TestChannelURLOverrideLines(t *testing.T) {
	logger.Pl.Console = io.Discard

	metaOps, _, err := sharedparsing.ParseMetaOps([]string{"title:prefix:[A] ", "artist:set:Cats"})
	if err != nil {
		t.Fatal(err)
	}
	filteredMetaOps, _, err := sharedparsing.ParseFilteredMetaOps([]string{"title:contains:cat||genre:set:Cats"})
	if err != nil {
		t.Fatal(err)
	}

	c := &models.Channel{ID: 1, Name: "Test"}
	cu := &models.ChannelURL{
		URL:             ownURL,
		ChanURLSettings: &models.Settings{VideoDir: "/videos/{{channel_name}}", JSONDir: "/json"},
		ChanURLMetarrArgs: &models.MetarrArgs{
			OutputDir:       "/out",
			MetaOps:         metaOps,
			FilteredMetaOps: filteredMetaOps,
		},
	}
	lines, err := ChannelURLOverrideLines(cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
	if err != nil {
		t.Fatal(err)
	}

	want := models.OverrideLines{
		VideoDir:        "/videos/{{channel_name}}", // Shown as written, as the user would enter it.
		JSONDir:         "/json",
		OutputDir:       "/out",
		MetaOps:         "title:prefix:[A] \nartist:set:Cats",
		FilteredMetaOps: "title:contains:cat||genre:set:Cats",
	}
	if lines != want {
		t.Errorf("got %+v\nwant %+v", lines, want)
	}

	if parsed, err := parsing.ParseMetaOps(strings.Split(lines.MetaOps, "\n")); err != nil || len(parsed) != 2 {
		t.Errorf("meta op lines parse back as %d ops, %v, want 2", len(parsed), err)
	}
	if parsed, err := parsing.ParseFilteredMetaOps(strings.Split(lines.FilteredMetaOps, "\n")); err != nil || len(parsed) != 1 {
		t.Errorf("filtered meta op lines parse back as %d ops, %v, want 1", len(parsed), err)
	}
}

// TestFileOverrideLines tests a Download As box gets an ops file's ops, with template tags filled in the path.
func TestFileOverrideLines(t *testing.T) {
	logger.Pl.Console = io.Discard
	dir := filepath.Join(t.TempDir(), "Test")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(filepath.Dir(dir), "{{channel_name}}", name)
	}
	c := &models.Channel{ID: 1, Name: "Test"}
	cu := &models.ChannelURL{URL: ownURL, ChanURLMetarrArgs: &models.MetarrArgs{}}
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	for _, tc := range []struct {
		box, content string
		want         []string
	}{
		{"meta_ops", "title:prefix:[A] \nartist:set:Cats\n", []string{"title:prefix:[A] ", "artist:set:Cats"}},
		{"filtered_meta_ops", "title:contains:cat||genre:set:Cats\n", []string{"title:contains:cat||genre:set:Cats"}},
		{"filename_ops", "date-tag:prefix:ymd\n", []string{"date-tag:prefix:ymd"}},
		{"filtered_filename_ops", "title:contains:cat||prefix:[Cats] \n", []string{"title:contains:cat||prefix:[Cats] "}},
	} {
		got, err := FileOverrideLines(tc.box, write(tc.box+".txt", tc.content), cu, dp)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, %v, want %q", tc.box, got, err, tc.want)
		}
	}

	if _, err := FileOverrideLines("meta_ops", write("bad.txt", "artist:bogus:x\n"), cu, dp); err == nil {
		t.Error("expected an error for an invalid op")
	}
	if _, err := FileOverrideLines("meta_ops", write("scoped.txt", ownURL+"|title:prefix:[A] \n"), cu, dp); err == nil {
		t.Error("expected an error for an op with a channel URL")
	}
	if _, err := FileOverrideLines("meta_ops", filepath.Join(dir, "missing.txt"), cu, dp); err == nil {
		t.Error("expected an error for a missing file")
	}
	if _, err := FileOverrideLines("ops", write("ops.txt", "artist:set:Cats\n"), cu, dp); err == nil {
		t.Error("expected an error for an unknown box")
	}
}

// TestParseMetarrOutputDirOverride tests a Download As output directory comes before move ops and the channel URL's.
func TestParseMetarrOutputDirOverride(t *testing.T) {
	logger.Pl.Console = io.Discard

	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{URL: ownURL, ChanURLMetarrArgs: &models.MetarrArgs{OutputDir: "/out"}}
	v := &models.Video{MoveOpOutputDir: "/moved", Overrides: &models.VideoOverrides{OutputDir: "/chosen/{{channel_name}}"}}
	if got := parseMetarrOutputDir(v, cu, parsing.NewDirectoryParser(c, cu, parsing.AllTags)); got != "/chosen/Cats" {
		t.Errorf("got %q, want /chosen/Cats", got)
	}
}

// TestOverrideFilenameOps tests a chosen filename replaces every other set op, and only matched filtered ops are added.
func TestOverrideFilenameOps(t *testing.T) {
	logger.Pl.Console = io.Discard

	parse := func(ops ...string) []models.FilenameOps {
		t.Helper()
		parsed, err := parsing.ParseFilenameOps(ops)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}
	v := &models.Video{
		URL:       "https://example.com/v/1",
		Overrides: &models.VideoOverrides{Filename: "My: Name", FilenameOps: parse("set:{{title}}", "date-tag:prefix:ymd")},
		FilteredFilenameOps: []models.FilteredFilenameOps{
			{FiltersMatched: true, FilenameOps: parse("set:{{id}}")},
			{FiltersMatched: false, FilenameOps: parse("suffix: [X]")},
		},
	}

	got := parse(overrideFilenameOps(v, &models.Channel{Name: "Test"})...)
	var sets []string
	for _, op := range got {
		if op.OpType == sharedconsts.OpSet {
			sets = append(sets, op.OpValue)
		}
	}
	if !slices.Equal(sets, []string{"My: Name"}) {
		t.Errorf("set ops: got %q, want only the chosen filename", sets)
	}
	if len(got) != 2 || got[0].OpType != sharedconsts.OpDateTag {
		t.Errorf("got %+v, want the date tag and the chosen filename", got)
	}
}

// TestMakeMetarrCommandOverrides tests a video's overrides replace the channel URL's ops, even when empty.
func TestMakeMetarrCommandOverrides(t *testing.T) {
	logger.Pl.Console = io.Discard

	channelOps, err := parsing.ParseMetaOps([]string{"artist:set:Channel"})
	if err != nil {
		t.Fatal(err)
	}
	c := &models.Channel{ID: 1, Name: "Test"}
	cu := &models.ChannelURL{URL: ownURL, ChanURLMetarrArgs: &models.MetarrArgs{MetaOps: channelOps}}
	v := &models.Video{URL: "https://example.com/v/1", VideoFilePath: "/v.mp4", JSONFilePath: "/v.json", Overrides: &models.VideoOverrides{}}

	args := makeMetarrCommand(v, cu, c, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
	for _, unwanted := range []string{"artist:set:Channel"} {
		if slices.Contains(args, unwanted) {
			t.Errorf("args %q hold %q, which the overrides replace", args, unwanted)
		}
	}
}
