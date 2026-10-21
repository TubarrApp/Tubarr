package downloads

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/logging"
)

// nilDBStore is a download store without a database, so Execute's domain block checks are skipped.
type nilDBStore struct{}

func (nilDBStore) GetDB() *sql.DB                                                  { return nil }
func (nilDBStore) SetDownloadStatus(*models.Video) error                           { return nil }
func (nilDBStore) UpdateDownloadStatus(context.Context, models.StatusUpdate) error { return nil }
func (nilDBStore) CancelDownload(int64, string) bool                               { return false }
func (nilDBStore) MarkDownloadCanceled(context.Context, int64) (bool, error)       { return false, nil }

// fakeYTDLP puts a yt-dlp script running body first on PATH, returning the file it adds a line to on each run.
func fakeYTDLP(t *testing.T, body string) (runs string) {
	t.Helper()
	dir := t.TempDir()
	runs = filepath.Join(dir, "runs")
	script := "#!/bin/sh\necho run >> '" + runs + "'\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "yt-dlp"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return runs
}

// countRuns returns how many times the fake yt-dlp ran.
func countRuns(t *testing.T, runs string) int {
	t.Helper()
	data, err := os.ReadFile(runs)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "run\n")
}

// newTestDownloads returns a video and a JSON download of url, with no wait between attempts.
func newTestDownloads(ctx context.Context, t *testing.T, url string, maxRetries int) (*VideoDownload, *JSONDownload) {
	t.Helper()
	opts := &Options{MaxRetries: maxRetries}
	cu := &models.ChannelURL{ChanURLSettings: &models.Settings{}}
	c := &models.Channel{ChanSettings: &models.Settings{}, ChanMetarrArgs: &models.MetarrArgs{}}
	tracker := &DownloadTracker{updates: make(chan models.StatusUpdate, 100), done: make(chan struct{})}
	vd, err := NewVideoDownload(ctx, &models.Video{URL: url, VideoDir: t.TempDir()}, cu, c, nilDBStore{}, tracker, opts)
	if err != nil {
		t.Fatal(err)
	}
	jd, err := NewJSONDownload(ctx, &models.Video{URL: url, JSONDir: t.TempDir()}, cu, c, nilDBStore{}, tracker, opts)
	if err != nil {
		t.Fatal(err)
	}
	return vd, jd
}

// TestExecuteRetries tests that failing downloads are tried MaxRetries times, then report every attempt failed.
func TestExecuteRetries(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	runs := fakeYTDLP(t, `echo "ERROR: video unavailable" >&2; exit 1`)
	vd, jd := newTestDownloads(t.Context(), t, "https://retries.example/v", 3)

	if _, err := vd.Execute(); err == nil || !strings.Contains(err.Error(), "all 3 download attempts failed") {
		t.Errorf("video download: got error %v, want all 3 attempts failed", err)
	}
	if n := countRuns(t, runs); n != 3 {
		t.Errorf("video download ran yt-dlp %d times, want 3", n)
	}

	if err := os.Remove(runs); err != nil {
		t.Fatal(err)
	}
	if _, err := jd.Execute(); err == nil || !strings.Contains(err.Error(), "all 3 JSON download attempts failed") {
		t.Errorf("JSON download: got error %v, want all 3 attempts failed", err)
	}
	if n := countRuns(t, runs); n != 3 {
		t.Errorf("JSON download ran yt-dlp %d times, want 3", n)
	}
}

// TestExecuteVideoSucceedsFirstTry tests that a successful video download isn't retried.
func TestExecuteVideoSucceedsFirstTry(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	videoPath := filepath.Join(t.TempDir(), "video.webm")
	if err := os.WriteFile(videoPath, []byte("not really a video"), 0o600); err != nil {
		t.Fatal(err)
	}
	runs := fakeYTDLP(t, "echo '"+videoPath+"'")
	vd, _ := newTestDownloads(t.Context(), t, "https://success.example/v", 3)

	if bot, err := vd.Execute(); err != nil || bot {
		t.Fatalf("got bot %t, error %v, want a clean success", bot, err)
	}
	if n := countRuns(t, runs); n != 1 {
		t.Errorf("ran yt-dlp %d times, want 1", n)
	}
	if vd.Video.VideoFilePath != videoPath {
		t.Errorf("VideoFilePath = %q, want %q", vd.Video.VideoFilePath, videoPath)
	}
}

// TestExecuteCanceledBeforeStart tests that a canceled download never runs yt-dlp.
func TestExecuteCanceledBeforeStart(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	runs := fakeYTDLP(t, "exit 1")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	vd, jd := newTestDownloads(ctx, t, "https://canceled.example/v", 3)

	if _, err := vd.Execute(); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Errorf("video download: got error %v, want a cancellation", err)
	}
	if _, err := jd.Execute(); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Errorf("JSON download: got error %v, want a cancellation", err)
	}
	if n := countRuns(t, runs); n != 0 {
		t.Errorf("ran yt-dlp %d times after cancellation, want 0", n)
	}
}
