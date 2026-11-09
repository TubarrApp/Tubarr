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
	"tubarr/internal/models"
)

// ErrCanceledByUser is the cause of a download context canceled from the web UI or API, as opposed to by shutdown.
var ErrCanceledByUser = errors.New("download canceled by user")

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
