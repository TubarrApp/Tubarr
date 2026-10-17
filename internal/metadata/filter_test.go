package metadata

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
)

// TestUnusableFilterFileSkipsVideo tests a filter file that can't be read stops a video downloading, as an error so it
// isn't marked ignored, rather than letting it through unfiltered.
func TestUnusableFilterFileSkipsVideo(t *testing.T) {
	logger.Pl.Console = io.Discard
	dir := t.TempDir()
	filterFile := filepath.Join(dir, "filters.txt")
	jsonFile := filepath.Join(dir, "video.json")
	if err := os.WriteFile(jsonFile, []byte(`{"title":"A video"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{
		URL:               "https://example.com/@cats",
		ChanURLSettings:   &models.Settings{FilterFile: filterFile},
		ChanURLMetarrArgs: &models.MetarrArgs{},
	}
	newVideo := func() *models.Video {
		return &models.Video{URL: "https://example.com/watch/1", JSONFilePath: jsonFile}
	}
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	// Missing file: skipped with an error.
	if pass, _, _, err := ValidateAndFilter(newVideo(), cu, c, dp); pass || !errors.Is(err, errFilterFile) {
		t.Errorf("missing filter file: passed = %v, err = %v; want not passed with errFilterFile", pass, err)
	}

	// Unparseable file: the same.
	if err := os.WriteFile(filterFile, []byte("not a filter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pass, _, _, err := ValidateAndFilter(newVideo(), cu, c, dp); pass || !errors.Is(err, errFilterFile) {
		t.Errorf("unparseable filter file: passed = %v, err = %v; want not passed with errFilterFile", pass, err)
	}

	// Chosen manually, so download filters (and the file) don't apply.
	v := newVideo()
	v.SkipFilters = true
	if pass, _, _, err := handleFilters(v, cu, c, dp); !pass || err != nil {
		t.Errorf("manual video: passed = %v, err = %v; want passed", pass, err)
	}

	// Empty file: no filters, so it passes.
	if err := os.WriteFile(filterFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if pass, _, _, err := handleFilters(newVideo(), cu, c, dp); !pass || err != nil {
		t.Errorf("empty filter file: passed = %v, err = %v; want passed", pass, err)
	}
}
