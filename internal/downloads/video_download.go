package downloads

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/downloads/downloaders"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// executeVideoDownload executes a video download command.
func (d *Download) executeVideoDownload(cmd *exec.Cmd) error {

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe error: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe error: %w", err)
	}
	filenameChan := make(chan string, 1)

	go d.scanVideoCmdOutput(io.MultiReader(stdout, stderr), filenameChan)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("command start error: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("command wait error: %w", err)
	}

	filename := <-filenameChan
	if filename == "" {
		return errors.New("no output filename captured")
	}
	d.Video.VPath = filename

	if err := d.waitForFile(d.Video.VPath, 5*time.Second); err != nil {
		return err
	}

	if err := verifyVideoDownload(d.Video.VPath); err != nil {
		return err
	}

	logging.S(0, "Download successful: %s", d.Video.VPath)
	return nil
}

// scanVideoCmdOutput scans the yt-dlp video download output for relevant information.
func (d *Download) scanVideoCmdOutput(r io.Reader, filenameChan chan<- string) {
	scanner := bufio.NewScanner(r)
	var (
		totalFrags,
		completedFrags int
		pct        float64
		err        error
		lastUpdate = models.StatusUpdate{
			VideoID:  d.Video.ID,
			VideoURL: d.Video.URL,
			Status:   consts.DLStatusPending,
			Percent:  0.0,
			Error:    nil,
		}
	)

	// Initialize as pending
	d.DLTracker.updates <- lastUpdate

	for scanner.Scan() {
		line := scanner.Text()
		switch d.DLTracker.downloader {

		// Aria2c
		case consts.DLerAria:
			totalFrags, completedFrags, pct, err = downloaders.Aria2OutputParser(line, d.Video.URL, totalFrags, completedFrags)
			if err != nil {
				logging.E(0, "Could not parse Aria2 output line %q: %v", line, err)
			}
		}

		// Send updates
		if pct > 0.0 {
			newUpdate := models.StatusUpdate{
				VideoID:  d.Video.ID,
				VideoURL: d.Video.URL,
				Status:   d.Video.DownloadStatus.Status,
				Percent:  pct,
				Error:    d.Video.DownloadStatus.Error,
			}
			if pct == 100.0 {
				newUpdate.Status = consts.DLStatusCompleted
			}
			if newUpdate != lastUpdate {
				d.Video.DownloadStatus.Status = newUpdate.Status
				d.Video.DownloadStatus.Pct = newUpdate.Percent
				d.DLTracker.sendUpdate(d)
				lastUpdate = newUpdate
			}
		}

		// Check for completed file path
		if strings.HasPrefix(line, "/") {
			ext := filepath.Ext(line)
			for _, validExt := range consts.AllVidExtensions {
				if ext == validExt {
					filenameChan <- line
					return
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		logging.E(0, "Scanner error: %v", err)
	}
	close(filenameChan)
}