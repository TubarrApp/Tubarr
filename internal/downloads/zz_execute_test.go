package downloads

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/vars"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/logging"
)

// nilDBStore is a download store without a database, so Execute's domain block checks are skipped.
type nilDBStore struct{}

func (nilDBStore) GetDB() *sql.DB                                                  { return nil }
func (nilDBStore) SetDownloadStatus(*models.Video) error                           { return nil }
func (nilDBStore) UpdateDownloadStatus(context.Context, models.StatusUpdate) error { return nil }

// statusTracker keeps a download's status on its video, as the download queue does.
type statusTracker struct{}

func (statusTracker) SetStatus(v *models.Video, status consts.DownloadStatus, pct float64, stage string, err error) {
	v.DownloadStatus = models.DLStatus{Status: status, Percent: pct, Stage: stage, Error: err}
}
func (statusTracker) SendUpdate(*models.Video) {}

// blockCheckStore is a download store with a database handle, so Execute's domain block checks run.
type blockCheckStore struct{ nilDBStore }

func (blockCheckStore) GetDB() *sql.DB { return &sql.DB{} }

// lockedBuffer is a goroutine-safe bytes.Buffer.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

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
func newTestDownloads(ctx context.Context, t *testing.T, url string, maxAttempts int) (*VideoDownload, *JSONDownload) {
	t.Helper()
	opts := &Options{MaxAttempts: maxAttempts}
	cu := &models.ChannelURL{ChanURLSettings: &models.Settings{}}
	c := &models.Channel{ChanSettings: &models.Settings{}, ChanMetarrArgs: &models.MetarrArgs{}}
	tracker := statusTracker{}
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

// TestExecuteRetries tests that failing downloads are tried MaxAttempts times, then report every attempt failed.
func TestExecuteRetries(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	runs := fakeYTDLP(t, `echo "ERROR: video unavailable" >&2; exit 1`)
	vd, jd := newTestDownloads(t.Context(), t, "https://retries.example/v", 3)

	if err := vd.Execute(); err == nil || !strings.Contains(err.Error(), "all 3 download attempts failed") {
		t.Errorf("video download: got error %v, want all 3 attempts failed", err)
	}
	if n := countRuns(t, runs); n != 3 {
		t.Errorf("video download ran yt-dlp %d times, want 3", n)
	}

	if err := os.Remove(runs); err != nil {
		t.Fatal(err)
	}
	if err := jd.Execute(); err == nil || !strings.Contains(err.Error(), "all 3 JSON download attempts failed") {
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

	if err := vd.Execute(); err != nil {
		t.Fatalf("got error %v, want a clean success", err)
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

	if err := vd.Execute(); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Errorf("video download: got error %v, want a cancellation", err)
	}
	if err := jd.Execute(); err == nil || !strings.Contains(err.Error(), "canceled") {
		t.Errorf("JSON download: got error %v, want a cancellation", err)
	}
	if n := countRuns(t, runs); n != 0 {
		t.Errorf("ran yt-dlp %d times after cancellation, want 0", n)
	}
}

// TestExecuteBlockedDomainSkipsYTDLP tests that downloads on an already blocked domain fail without running yt-dlp.
func TestExecuteBlockedDomainSkipsYTDLP(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	vars.BlockedDomainsMutex.Lock()
	origBlocked := vars.BlockedDomains
	vars.BlockedDomains = map[string]map[vars.BlockContext]time.Time{
		"blocked.example": {vars.BlockContextUnauth: time.Now()},
	}
	vars.BlockedDomainsMutex.Unlock()
	t.Cleanup(func() {
		vars.BlockedDomainsMutex.Lock()
		vars.BlockedDomains = origBlocked
		vars.BlockedDomainsMutex.Unlock()
	})

	runs := fakeYTDLP(t, "exit 1")
	opts := &Options{MaxAttempts: 3}
	cu := &models.ChannelURL{ChanURLSettings: &models.Settings{}}
	c := &models.Channel{ChanSettings: &models.Settings{}, ChanMetarrArgs: &models.MetarrArgs{}}
	tracker := statusTracker{}
	url := "https://www.blocked.example/v"

	vd, err := NewVideoDownload(t.Context(), &models.Video{URL: url, VideoDir: t.TempDir()}, cu, c, blockCheckStore{}, tracker, opts)
	if err != nil {
		t.Fatal(err)
	}
	jd, err := NewJSONDownload(t.Context(), &models.Video{URL: url, JSONDir: t.TempDir()}, cu, c, blockCheckStore{}, tracker, opts)
	if err != nil {
		t.Fatal(err)
	}

	if err := jd.Execute(); err == nil || !strings.Contains(err.Error(), "is blocked") {
		t.Errorf("JSON download: got error %v, want a domain block", err)
	}
	if err := vd.Execute(); err == nil || !strings.Contains(err.Error(), "is blocked") {
		t.Errorf("video download: got error %v, want a domain block", err)
	}
	if n := countRuns(t, runs); n != 0 {
		t.Errorf("ran yt-dlp %d times on a blocked domain, want 0", n)
	}
}

// TestCancelRemovesPartials tests that a user cancel removes only the partials of reported destinations in the video directory,
// and that a shutdown keeps them for resuming.
func TestCancelRemovesPartials(t *testing.T) {
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: &lockedBuffer{}}
	t.Cleanup(func() { logger.Pl = origLogger })

	for _, tc := range []struct {
		name        string
		cause       error
		wantRemoved bool
	}{
		{"user cancel", ErrCanceledByUser, true},
		{"shutdown", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, outside := t.TempDir(), t.TempDir()
			dest := filepath.Join(dir, "video.f137.mp4")
			outsideDest := filepath.Join(outside, "elsewhere.mp4")
			kept := []string{filepath.Join(dir, "other.mp4.part"), outsideDest + ".part"}
			for _, p := range kept {
				if err := os.WriteFile(p, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}

			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)
			d := &VideoDownload{
				Video:      &models.Video{ID: 800001, URL: "https://a.example/v", VideoDir: dir},
				Channel:    &models.Channel{},
				ChannelURL: &models.ChannelURL{ChanURLSettings: &models.Settings{}},
				DLTracker:  statusTracker{},
				Context:    ctx,
			}

			script := fmt.Sprintf(`echo "[download] Destination: %s"; echo "[download] Destination: %s"; : > %q; : > %q; exec sleep 30`,
				dest, outsideDest, dest+".part", dest+".ytdl")
			cmd := exec.CommandContext(ctx, "sh", "-c", script)
			useProcessGroup(cmd)
			done := make(chan error, 1)
			go func() { done <- d.executeVideoDownload(cmd) }()

			for start := time.Now(); ; time.Sleep(10 * time.Millisecond) {
				if _, err := os.Stat(dest + ".ytdl"); err == nil {
					break
				}
				if time.Since(start) > 10*time.Second {
					t.Fatal("fake yt-dlp never wrote its partials")
				}
			}
			if tc.cause != nil {
				cancel(tc.cause)
			} else {
				cancel(context.Canceled)
			}
			if err := <-done; err == nil {
				t.Fatal("expected an error from a canceled download")
			}
			_ = d.cancelVideoDownload()

			for _, p := range []string{dest + ".part", dest + ".ytdl"} {
				_, err := os.Stat(p)
				if removed := os.IsNotExist(err); removed != tc.wantRemoved {
					t.Errorf("%s: removed = %v, want %v", filepath.Base(p), removed, tc.wantRemoved)
				}
			}
			for _, p := range kept {
				if _, err := os.Stat(p); err != nil {
					t.Errorf("%q was removed although it wasn't this download's partial in the video directory", p)
				}
			}
		})
	}
}

// TestExecuteVideoDownloadTrailingOutput checks output after the final filename is fully read before cmd.Wait closes the pipes.
func TestExecuteVideoDownloadTrailingOutput(t *testing.T) {
	logs := &lockedBuffer{}
	origLogger := logger.Pl
	logger.Pl = &logging.ProgramLogger{Console: logs}
	t.Cleanup(func() { logger.Pl = origLogger })

	videoPath := filepath.Join(t.TempDir(), "video.webm")
	if err := os.WriteFile(videoPath, []byte("not really a video"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A burst fitting the OS pipe buffer lets the process exit with lines still unread (the original race).
	// A burst larger than the pipe buffer hangs if the parser stops draining after completion.
	for _, trailing := range []int{3000, 200000} {
		for i := range 10 {
			t.Run(fmt.Sprintf("%d_lines_run_%d", trailing, i), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
				defer cancel()

				script := fmt.Sprintf(`echo %q; seq %d; seq %d >&2`, videoPath, trailing, trailing)
				d := &VideoDownload{
					Video:      &models.Video{ID: int64(900000 + trailing + i), URL: "https://a.example/v"},
					Channel:    &models.Channel{},
					ChannelURL: &models.ChannelURL{ChanURLSettings: &models.Settings{}},
					DLTracker:  statusTracker{},
					Context:    ctx,
				}

				done := make(chan error, 1)
				go func() { done <- d.executeVideoDownload(exec.CommandContext(ctx, "sh", "-c", script)) }()
				select {
				case err := <-done:
					if err != nil {
						t.Fatalf("executeVideoDownload() error = %v", err)
					}
				case <-ctx.Done():
					t.Fatal("executeVideoDownload() hung")
				}

				if d.Video.VideoFilePath != videoPath {
					t.Errorf("VideoFilePath = %q, want %q", d.Video.VideoFilePath, videoPath)
				}
				if strings.Contains(logs.String(), "Scanner error") {
					t.Fatalf("pipe read after close:\n%s", logs.String())
				}
			})
		}
	}
}
