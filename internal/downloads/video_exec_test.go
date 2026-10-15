package downloads

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"

	"github.com/TubarrApp/gocommon/logging"
)

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
					DLTracker:  &DownloadTracker{updates: make(chan models.StatusUpdate, 100), done: make(chan struct{})},
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
