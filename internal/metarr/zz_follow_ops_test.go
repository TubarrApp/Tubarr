package metarr

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
)

// TestFollowChannelURLOps tests the channel URL's lines in saved Download As ops are refreshed from its ops and ops
// files as they are now, keeping the lines added or deleted for the video.
func TestFollowChannelURLOps(t *testing.T) {
	logger.Pl.Console = io.Discard
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	filtered := func(lines ...string) []models.FilteredMetaOps {
		t.Helper()
		ops, err := parsing.ParseFilteredMetaOps(lines)
		if err != nil {
			t.Fatal(err)
		}
		return ops
	}

	metaOpsFile := write("meta.txt", "album:set:Old\ngenre:set:Pets\n")
	c := &models.Channel{ID: 1, Name: "Test"}
	cu := withOwnLevel(&models.ChannelURL{URL: ownURL, ChanURLSettings: &models.Settings{}, ChanURLMetarrArgs: &models.MetarrArgs{
		MetaOpsFile:     metaOpsFile,
		FilenameOpsFile: write("filename.txt", "suffix: [Old]\n"),
		FilteredMetaOps: filtered("title:contains:cat||genre:set:Cats"),
	}})
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	// Saved with a meta op deleted, the filtered meta box left as the channel URL's, and a filename op added.
	was, err := ChannelURLOpLines(cu, dp)
	if err != nil {
		t.Fatal(err)
	}
	if was.MetaOps != "album:set:Old\ngenre:set:Pets" {
		t.Fatalf("channel URL's meta ops %q, want the file's", was.MetaOps)
	}
	o := &models.VideoOverrides{ChannelURLOps: was}
	if o.MetaOps, err = parsing.ParseMetaOps([]string{"album:set:Old"}); err != nil {
		t.Fatal(err)
	}
	o.FilteredMetaOps = filtered(opLines(was.FilteredMetaOps)...)
	if o.FilenameOps, err = parsing.ParseFilenameOps(append(opLines(was.FilenameOps), "prefix:[Mine] ")); err != nil {
		t.Fatal(err)
	}

	// The channel URL's ops files and typed ops change.
	write("meta.txt", "album:set:New\ngenre:set:Pets\n")
	write("filename.txt", "suffix: [New]\n")
	cu.ChanURLMetarrArgs.FilteredMetaOps = filtered("title:contains:dog||genre:set:Dogs")
	now, err := ChannelURLOpLines(cu, dp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(now.MetaOps, "album:set:New") || strings.Contains(now.MetaOps, "Old") {
		t.Fatalf("channel URL's meta ops now %q, want the file's new op", now.MetaOps)
	}

	// Following twice gives the same ops.
	for range 2 {
		if err := FollowChannelURLOps(o, cu, dp); err != nil {
			t.Fatal(err)
		}
		got := VideoOverrideLines(o)
		if want := "album:set:New"; got.MetaOps != want {
			t.Errorf("box with a deleted op: got %q, want %q, the file's new op in and the deleted one still out", got.MetaOps, want)
		}
		if got.FilteredMetaOps != now.FilteredMetaOps {
			t.Errorf("box left as the channel URL's: got %q, want its ops now, %q", got.FilteredMetaOps, now.FilteredMetaOps)
		}
		if want := "prefix:[Mine] \nsuffix: [New]"; got.FilenameOps != want {
			t.Errorf("box with an added op: got %q, want %q, the added op kept and the file's suffix swapped", got.FilenameOps, want)
		}
		if *o.ChannelURLOps != *now {
			t.Errorf("channel URL's ops kept as %+v, want %+v", *o.ChannelURLOps, *now)
		}
	}

	// Overrides without the channel URL's ops stay as saved.
	old, err := parsing.ParseMetaOps([]string{"album:set:Old"})
	if err != nil {
		t.Fatal(err)
	}
	kept := &models.VideoOverrides{MetarrArgs: models.MetarrArgs{MetaOps: old}}
	if err := FollowChannelURLOps(kept, cu, dp); err != nil || VideoOverrideLines(kept).MetaOps != "album:set:Old" {
		t.Errorf("overrides without the channel URL's ops: got %q, %v, want them as saved", VideoOverrideLines(kept).MetaOps, err)
	}

	// An ops file that can't be read is an error.
	if err := os.Remove(metaOpsFile); err != nil {
		t.Fatal(err)
	}
	if err := FollowChannelURLOps(o, cu, dp); err == nil {
		t.Error("expected an error for a missing ops file")
	}
}
