package downloads

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/limits"
	"tubarr/internal/models"
)

// ErrCanceledByUser is the cause of a download context canceled from the web UI or API, as opposed to by shutdown.
var ErrCanceledByUser = errors.New("download canceled by user")

// globalDownloadSem limits concurrent video downloads across all channels.
// nil means no global limit (unlimited concurrent downloads).
var globalDownloadSem chan struct{}
var globalDownloadSemMu sync.RWMutex

// Tracker is told a download's status whenever it changes.
type Tracker interface {
	// SetStatus sets the video's download status, then reports it.
	SetStatus(v *models.Video, status consts.DownloadStatus, pct float64, stage string, err error)
	// SendUpdate reports the video's download status as it now is.
	SendUpdate(v *models.Video)
}

// Options holds configuration for download operations.
type Options struct {
	MaxAttempts      int
	RetryMaxInterval int
}

// DefaultOptions provides sensible defaults.
var DefaultOptions = Options{
	MaxAttempts:      1 + consts.DefaultDownloadRetries,
	RetryMaxInterval: 20,
}

// VideoDownload encapsulates a video download operation.
type VideoDownload struct {
	Video      *models.Video
	ChannelURL *models.ChannelURL
	Channel    *models.Channel
	DLStore    contracts.DownloadStore
	DLTracker  Tracker
	Options    Options
	Context    context.Context

	// Private
	cmd          *exec.Cmd
	mu           sync.Mutex
	fsGen        int      // FlareSolverr solution generation the last command used.
	destinations []string // Files yt-dlp reported downloading to, across attempts.
}

// JSONDownload encapsulates a JSON download operation.
type JSONDownload struct {
	Video      *models.Video
	Channel    *models.Channel
	ChannelURL *models.ChannelURL
	DLStore    contracts.DownloadStore
	DLTracker  Tracker
	Options    Options
	Context    context.Context

	// Private
	cmd   *exec.Cmd
	mu    sync.Mutex
	fsGen int // FlareSolverr solution generation the last command used.
}

// cleanup safely terminates any running command.
func (d *VideoDownload) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Kill running command if exists.
	if d.cmd != nil && d.cmd.Process != nil {
		stopProcess(d.cmd.Process, "video")
		d.cmd = nil
	}
}

// cleanup safely terminates any running command.
func (d *JSONDownload) cleanup() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Kill running command if exists.
	if d.cmd != nil && d.cmd.Process != nil {
		stopProcess(d.cmd.Process, "JSON")
		d.cmd = nil
	}
}

// InitGlobalDownloadLimit sets up the global download concurrency limiter.
// A limit of 0 means no limit (unlimited concurrent downloads).
// Note: Changes take effect for new downloads. In-progress downloads retain their slots.
func InitGlobalDownloadLimit(limit int) {
	globalDownloadSemMu.Lock()
	defer globalDownloadSemMu.Unlock()

	if limit > 0 {
		globalDownloadSem = make(chan struct{}, limit)
		logger.Pl.I("Global download concurrency limit set to %d", limit)
	} else {
		globalDownloadSem = nil
		logger.Pl.I("Global download concurrency limit disabled (unlimited)")
	}
}

// AcquireGlobalDownloadSlot blocks until a download slot is available, or ctx is done.
// Returns the semaphore channel that was used (for proper release later).
// Returns immediately if no global limit is configured (limit=0).
func AcquireGlobalDownloadSlot(ctx context.Context, videoURL string) (chan struct{}, error) {
	globalDownloadSemMu.RLock()
	sem := globalDownloadSem
	globalDownloadSemMu.RUnlock()

	if sem == nil {
		return nil, nil
	}
	if err := limits.AcquireSlot(ctx, sem, "global download", videoURL); err != nil {
		return nil, err
	}
	return sem, nil
}

// isProcessNotExist checks if an error indicates a process doesn't exist.
func isProcessNotExist(err error) bool {
	return errors.Is(err, syscall.ESRCH)
}

// stopProcess interrupts a download process, killing it if that fails.
func stopProcess(p *os.Process, what string) {
	if err := p.Signal(os.Interrupt); err == nil {
		return
	}
	// Only log if it's not "process already finished" or "no such process".
	if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) && !isProcessNotExist(err) {
		logger.Pl.W("Failed to cleanup %s download process %d: %v", what, p.Pid, err)
	}
}
