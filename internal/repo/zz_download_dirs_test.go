package repo

import (
	"os"
	"path/filepath"
	"testing"
	"tubarr/internal/models"
)

// TestCreateDownloadDirs tests that saving creates only the plain folders the save adds, so a missing folder (e.g. on an
// unmounted drive) is never recreated by saving unchanged settings.
func TestCreateDownloadDirs(t *testing.T) {
	dir := t.TempDir()
	videos, json, moved := filepath.Join(dir, "videos"), filepath.Join(dir, "json"), filepath.Join(dir, "moved")
	exists := func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}

	// New settings: every plain folder is created, templated ones aren't.
	s := &models.Settings{VideoDir: videos, JSONDir: filepath.Join(dir, "{{channel_name}}")}
	if err := createDownloadDirs(nil, s); err != nil {
		t.Fatal(err)
	}
	if !exists(videos) || exists(filepath.Join(dir, "{{channel_name}}")) {
		t.Fatalf("new settings: video folder exists: %t, templated folder exists: %t; want true, false", exists(videos), exists(filepath.Join(dir, "{{channel_name}}")))
	}

	// Saved again unchanged while the folder is missing: not recreated.
	if err := os.Remove(videos); err != nil {
		t.Fatal(err)
	}
	if err := createDownloadDirs(s, s); err != nil {
		t.Fatal(err)
	}
	if exists(videos) {
		t.Error("saving unchanged settings recreated the missing video folder")
	}

	// A new JSON folder and move op folder are created, the unchanged missing video folder still isn't.
	updated := &models.Settings{VideoDir: videos, JSONDir: json, MetaFilterMoveOps: []models.MetaFilterMoveOps{{Field: "title", ContainsValue: "cats", OutputDir: moved}}}
	if err := createDownloadDirs(s, updated); err != nil {
		t.Fatal(err)
	}
	if !exists(json) || !exists(moved) || exists(videos) {
		t.Errorf("after adding folders: JSON %t, move op %t, video %t; want true, true, false", exists(json), exists(moved), exists(videos))
	}
}
