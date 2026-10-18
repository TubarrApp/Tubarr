package metarr

import (
	"io"
	"slices"
	"strings"
	"testing"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"

	"github.com/TubarrApp/gocommon/sharedconsts"
	"github.com/spf13/viper"
)

const (
	ownURL   = "https://example.com/@a"
	otherURL = "https://example.com/@b"
)

// TestChannelURLOpLines tests the Download As dialog gets the channel URL's ops without their channel URL prefix, leaves
// out other channel URLs' ops, falls back to the global ops like Metarr does, and that its lines parse back.
func TestChannelURLOpLines(t *testing.T) {
	logger.Pl.Console = io.Discard
	t.Cleanup(viper.Reset)

	metaOps, err := parsing.ParseMetaOps([]string{ownURL + "|title:prefix:[A] ", otherURL + "|title:prefix:[B] ", "artist:set:Cats"})
	if err != nil {
		t.Fatal(err)
	}
	filenameOps, err := parsing.ParseFilenameOps([]string{otherURL + "|date-tag:prefix:ymd"})
	if err != nil {
		t.Fatal(err)
	}
	filteredMetaOps, err := parsing.ParseFilteredMetaOps([]string{ownURL + "|title:contains:cat|genre:set:Cats", otherURL + "|title:contains:dog|genre:set:Dogs"})
	if err != nil {
		t.Fatal(err)
	}
	viper.Set(keys.MFilenameOps, []string{ownURL + "|append: [G]", otherURL + "|append: [H]"})

	c := &models.Channel{ID: 1, Name: "Test"}
	cu := &models.ChannelURL{URL: ownURL, ChanURLMetarrArgs: &models.MetarrArgs{
		MetaOps:         metaOps,
		FilenameOps:     filenameOps,
		FilteredMetaOps: filteredMetaOps,
	}}
	lines, err := ChannelURLOpLines(cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
	if err != nil {
		t.Fatal(err)
	}

	want := models.OverrideLines{
		MetaOps:         "title:prefix:[A] \nartist:set:Cats",
		FilteredMetaOps: "title:contains:cat|genre:set:Cats",
		FilenameOps:     "append: [G]", // The channel URL has none of its own.
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
			{FiltersMatched: false, FilenameOps: parse("append: [X]")},
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

// TestMakeMetarrCommandOverrides tests a video's overrides replace the channel URL's and global ops, even when empty.
func TestMakeMetarrCommandOverrides(t *testing.T) {
	logger.Pl.Console = io.Discard
	t.Cleanup(viper.Reset)
	viper.Set(keys.MMetaOps, []string{"artist:set:Global"})
	viper.Set(keys.MFilenameOps, []string{"append: [Global]"})

	channelOps, err := parsing.ParseMetaOps([]string{"artist:set:Channel"})
	if err != nil {
		t.Fatal(err)
	}
	c := &models.Channel{ID: 1, Name: "Test"}
	cu := &models.ChannelURL{URL: ownURL, ChanURLMetarrArgs: &models.MetarrArgs{MetaOps: channelOps}}
	v := &models.Video{URL: "https://example.com/v/1", VideoFilePath: "/v.mp4", JSONFilePath: "/v.json", Overrides: &models.VideoOverrides{}}

	args := makeMetarrCommand(v, cu, c, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags))
	for _, unwanted := range []string{"artist:set:Global", "append: [Global]", "artist:set:Channel"} {
		if slices.Contains(args, unwanted) {
			t.Errorf("args %q hold %q, which the overrides replace", args, unwanted)
		}
	}
	if !slices.Contains(args, defaultFilenameSet) {
		t.Errorf("args %q lack the default filename set", args)
	}
}
