package downloads

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tubarr/internal/domain/cmdvideo"
	"tubarr/internal/domain/consts"
	"tubarr/internal/downloads/downloaders"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

const (
	ariaBase = len(consts.DownloaderAria) + len(": ") + len(cmdvideo.AriaLog)
)

// buildVideoCommand builds the command to download a video using yt-dlp.
func (d *Download) buildVideoCommand() *exec.Cmd {
	args := make([]string, 0, 32)

	args = append(args,
		cmdvideo.RestrictFilenames,
		cmdvideo.Output, filepath.Join(d.Video.VideoDir, cmdvideo.FilenameSyntax))

	args = append(args, cmdvideo.Print, cmdvideo.AfterMove)

	if d.Video.Settings.CookieSource != "" {
		args = append(args, cmdvideo.CookieSource, d.Video.Settings.CookieSource)
	}

	if d.Video.Settings.MaxFilesize != "" {
		args = append(args, cmdvideo.MaxFilesize, d.Video.Settings.MaxFilesize)
	}

	if d.Video.Settings.ExternalDownloader != "" {
		args = append(args, cmdvideo.ExternalDLer, d.Video.Settings.ExternalDownloader)
		if d.Video.Settings.ExternalDownloaderArgs != "" {

			switch d.Video.Settings.ExternalDownloader {
			case consts.DownloaderAria:
				var b strings.Builder

				b.Grow(ariaBase + len(d.Video.Settings.ExternalDownloaderArgs))
				b.WriteString(consts.DownloaderAria)
				b.WriteRune(':')
				b.WriteString(d.Video.Settings.ExternalDownloaderArgs) // "aria2c:-x 16 -s 16 --console-log-level=info"
				b.WriteRune(' ')
				b.WriteString(cmdvideo.AriaLog)

				args = append(args, cmdvideo.ExternalDLArgs, b.String())
			default:
				args = append(args, cmdvideo.ExternalDLArgs, d.Video.Settings.ExternalDownloaderArgs)
			}
		}
	}

	if d.Video.Settings.Retries != 0 {
		args = append(args, cmdvideo.Retries, strconv.Itoa(d.Video.Settings.Retries))
	}

	args = append(args, cmdvideo.SleepRequests, cmdvideo.SleepRequestsNum, d.Video.URL)

	cmd := exec.CommandContext(d.Context, cmdvideo.YTDLP, args...)
	logging.D(1, "Built video download command for URL %q:\n%v", d.Video.URL, cmd.String())

	return cmd
}

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
	d.Video.VideoPath = filename

	if err := d.waitForFile(d.Video.VideoPath, 5*time.Second); err != nil {
		return err
	}

	if err := verifyVideoDownload(d.Video.VideoPath); err != nil {
		return err
	}

	logging.S(0, "Download successful: %s", d.Video.VideoPath)
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
		case consts.DownloaderAria:
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
				d.DLTracker.sendUpdate(d.Video)
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