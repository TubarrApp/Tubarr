package app

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"tubarr/internal/domain/consts"
	"tubarr/internal/downloads"
	"tubarr/internal/models"
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
