package app

import (
	"context"
	"path/filepath"
	"testing"
	"tubarr/internal/database"
	"tubarr/internal/domain/paths"
	"tubarr/internal/models"
	"tubarr/internal/repo"
	"tubarr/internal/scraper"
)

// TestUnusableFilterFileHoldsBackCrawledVideos tests crawled videos aren't processed at all while the filter file can't be
// used, so nothing downloads unfiltered and no requests are made for them.
func TestUnusableFilterFileHoldsBackCrawledVideos(t *testing.T) {
	paths.HomeTubarrDir = t.TempDir()
	paths.DBFilePath = filepath.Join(paths.HomeTubarrDir, "tubarr.db")
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	store, err := repo.InitStores(db)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	settings := func() *models.Settings {
		return &models.Settings{VideoDir: dir, JSONDir: dir, FilterFile: filepath.Join(dir, "missing-filters.txt")}
	}
	c := &models.Channel{ID: 1, Name: "Cats", ChanSettings: settings(), ChanMetarrArgs: &models.MetarrArgs{}}
	cu := &models.ChannelURL{ID: 1, URL: "https://example.com/@cats", ChanURLSettings: settings(), ChanURLMetarrArgs: &models.MetarrArgs{}}
	videos := []*models.Video{{URL: "https://example.com/watch/1"}, {URL: "https://example.com/watch/2"}}

	succeeded, downloaded, err := InitProcess(context.Background(), store, c, cu, videos, scraper.New())
	if err == nil || succeeded != 0 || downloaded != 0 {
		t.Fatalf("InitProcess = %d succeeded, %d downloaded, %v; want none processed and an error", succeeded, downloaded, err)
	}
	for _, v := range videos {
		if !v.SkipDownloads.Skip || v.SkipDownloads.Reason != models.SkipDLMissingFilterFile {
			t.Errorf("video %q SkipDownloads = %+v, want skipped for the missing filter file", v.URL, v.SkipDownloads)
		}
	}
}
