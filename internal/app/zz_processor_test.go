package app

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"testing"
	"tubarr/internal/database"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/paths"
	"tubarr/internal/downloads"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/repo"
	"tubarr/internal/scraper"
)

// recordingDownloadStore records the download statuses stored for each video.
type recordingDownloadStore struct {
	mu       sync.Mutex
	statuses map[int64]consts.DownloadStatus
}

func (s *recordingDownloadStore) GetDB() *sql.DB                        { return nil }
func (s *recordingDownloadStore) SetDownloadStatus(*models.Video) error { return nil }
func (s *recordingDownloadStore) CancelDownload(int64, string) bool     { return false }
func (s *recordingDownloadStore) UpdateDownloadStatus(_ context.Context, u models.StatusUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses[u.VideoID] = u.Status
	return nil
}
func (s *recordingDownloadStore) MarkDownloadCanceled(context.Context, int64) (bool, error) {
	return false, nil
}

// TestFinishTracking tests that a tracked video always leaves the Downloads page with a final status, and one that
// left the pipeline unfinished (e.g. skipped as a duplicate download) returns to the Available list.
func TestFinishTracking(t *testing.T) {
	errDL := errors.New("download failed")
	tests := []struct {
		name      string
		videoID   int64
		endStatus consts.DownloadStatus // Status the pipeline left the video in.
		err       error
		cancel    error // Cause the video's context was canceled with, if any.
		want      consts.DownloadStatus
	}{
		{"skipped duplicate", 9_000_001, consts.DLStatusQueued, nil, nil, consts.DLStatusFailed},
		{"failed while queued", 9_000_002, consts.DLStatusQueued, errDL, nil, consts.DLStatusFailed},
		{"failed mid-download", 9_000_003, consts.DLStatusDownloading, errDL, nil, consts.DLStatusFailed},
		{"canceled by user", 9_000_004, consts.DLStatusQueued, errDL, downloads.ErrCanceledByUser, consts.DLStatusCanceled},
		{"stopped by shutdown", 9_000_005, consts.DLStatusQueued, errDL, context.Canceled, consts.DLStatusFailed},
		{"ignored by filters", 9_000_006, consts.DLStatusIgnored, nil, nil, consts.DLStatusIgnored},
		{"downloaded", 9_000_007, consts.DLStatusCompleted, nil, nil, consts.DLStatusCompleted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &recordingDownloadStore{statuses: map[int64]consts.DownloadStatus{}}
			tracker := downloads.NewDownloadTracker(store)
			tracker.Start(context.Background())

			v := &models.Video{ID: tt.videoID, URL: "https://example.com/v"}
			tracker.SetStatus(v, consts.DLStatusQueued, 0, "Waiting to start", nil)
			v.DownloadStatus.Status = tt.endStatus

			vidCtx, vidCancel := context.WithCancelCause(context.Background())
			defer vidCancel(nil)
			if tt.cancel != nil {
				vidCancel(tt.cancel)
			}

			finishTracking(vidCtx, tracker, v, tt.err)
			tracker.Stop()

			if got := store.statuses[tt.videoID]; got != tt.want {
				t.Errorf("stored status %q, want %q", got, tt.want)
			}
		})
	}
}

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

// TestOverrideDir tests a Download As directory is filled in, a blank one keeps the channel URL's, and one whose
// fixed part doesn't exist (e.g. an unmounted drive) is refused.
func TestOverrideDir(t *testing.T) {
	logger.Pl.Console = io.Discard
	dir := t.TempDir()
	c := &models.Channel{ID: 1, Name: "Cats"}
	cu := &models.ChannelURL{URL: "https://example.com/@cats"}
	dp := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	if got, err := overrideDir(dp, "", "/channel", "test"); err != nil || got != "/channel" {
		t.Errorf("blank: got %q, %v, want /channel", got, err)
	}
	if got, err := overrideDir(dp, filepath.Join(dir, "{{channel_name}}"), "/channel", "test"); err != nil || got != filepath.Join(dir, "Cats") {
		t.Errorf("templated: got %q, %v, want %q", got, err, filepath.Join(dir, "Cats"))
	}
	if _, err := overrideDir(dp, filepath.Join(dir, "unmounted", "videos"), "/channel", "test"); err == nil {
		t.Error("missing destination: got no error")
	}
}
