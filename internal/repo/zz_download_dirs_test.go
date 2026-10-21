package repo

import (
	"os"
	"path/filepath"
	"testing"
	"tubarr/internal/models"
)

// TestCheckDownloadDirs tests that saving refuses a missing folder it adds without creating it, but accepts unchanged
// ones, so e.g. a channel can be saved while an unchanged folder's drive is unmounted.
func TestCheckDownloadDirs(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	exists := func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}

	// Existing and templated (with an existing fixed part) folders are accepted.
	s := &models.Settings{VideoDir: dir, JSONDir: filepath.Join(dir, "{{channel_name}}")}
	if err := checkDownloadDirs(nil, s); err != nil {
		t.Fatalf("existing folders: %v", err)
	}

	// New missing folders are refused and not created, whether plain, templated or a move op's.
	for name, added := range map[string]*models.Settings{
		"video":     {VideoDir: missing},
		"templated": {JSONDir: filepath.Join(missing, "{{channel_name}}")},
		"move op":   {MetaFilterMoveOps: []models.MetaFilterMoveOps{{Field: "title", Value: "cats", OutputDir: missing}}},
	} {
		if err := checkDownloadDirs(nil, added); err == nil {
			t.Errorf("%s: missing folder accepted", name)
		}
	}
	if exists(missing) {
		t.Fatal("checking created the missing folder")
	}

	// Saved again unchanged while a folder is missing: accepted, and still not created.
	unchanged := &models.Settings{VideoDir: missing}
	if err := checkDownloadDirs(unchanged, unchanged); err != nil {
		t.Errorf("unchanged missing folder: %v", err)
	}

	// Metarr output folder: a new missing one is refused, an unchanged one accepted.
	if err := checkOutputDir(nil, &models.MetarrArgs{OutputDir: missing}); err == nil {
		t.Error("output folder: missing folder accepted")
	}
	if err := checkOutputDir(&models.MetarrArgs{OutputDir: missing}, &models.MetarrArgs{OutputDir: missing}); err != nil {
		t.Errorf("output folder: unchanged missing folder: %v", err)
	}
	if exists(missing) {
		t.Error("checking created the missing folder")
	}
}
