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

	"tubarr/internal/cfg"
	"tubarr/internal/domain/cmdvideo"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/errconsts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/downloads/downloaders"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

const (
	ariaBase = len(consts.DownloaderAria) + len(": ") + len(cmdvideo.AriaLog)
)

type VideoDownloadState struct {
	TotalFrags     int
	CompletedFrags int
	URL            string
}

var (
	states = make(map[string]*VideoDownloadState)
)

// buildVideoCommand builds the command to download a video using yt-dlp.
func (d *VideoDownload) buildVideoCommand() *exec.Cmd {
	args := make([]string, 0, 32)

	args = append(args, cmdvideo.RestrictFilenames)

	var outputSyntax string
	if d.Video.JSONCustomFile != "" {
		JSONFileName := filepath.Base(d.Video.JSONCustomFile)
		outputSyntax = strings.TrimSuffix(JSONFileName, ".json") + ".%(ext)s"
	} else {
		outputSyntax = cmdvideo.FilenameSyntax
	}

	args = append(args, cmdvideo.Output, filepath.Join(d.Video.VideoDir, outputSyntax))
	args = append(args, cmdvideo.Print, cmdvideo.AfterMove)

	if d.Video.CookiePath == "" {
		if d.Video.Settings.CookieSource != "" {
			args = append(args, cmdvideo.CookieSource, d.Video.Settings.CookieSource)
		}
	} else {
		args = append(args, cmdvideo.CookiePath, d.Video.CookiePath)
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
				b.WriteByte(':')
				b.WriteString(d.Video.Settings.ExternalDownloaderArgs) // "aria2c:-x 16 -s 16 --console-log-level=info"
				b.WriteByte(' ')
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

	args = append(args, cmdvideo.SleepRequests, cmdvideo.SleepRequestsNum)
	args = append(args, cmdvideo.RandomizeRequests...)

	if cfg.IsSet(keys.TubarrCookieSource) {
		browser := cfg.GetString(keys.TubarrCookieSource)
		logging.I("Using cookies from browser %q", browser)
		args = append(args, "--cookies-from-browser", browser)
	} else {
		logging.D(1, "No browser cookies set for Tubarr, skipping")
	}

	if d.Video.Channel.Settings.YtdlpOutputExt != "" {
		args = append(args, cmdvideo.YtdlpOutputExtension, d.Video.Channel.Settings.YtdlpOutputExt)
	}

	if d.Video.DirectVideoURL != "" {
		args = append(args, d.Video.DirectVideoURL)
	} else {
		args = append(args, d.Video.URL)
	}

	cmd := exec.CommandContext(d.Context, cmdvideo.YTDLP, args...)
	logging.D(1, "Built video download command for URL %q:\n%v", d.Video.URL, cmd.String())

	return cmd
}

// executeVideoDownload executes a video download command.
func (d *VideoDownload) executeVideoDownload(cmd *exec.Cmd) error {

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
		return fmt.Errorf(errconsts.YTDLPFailure, err)
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf(errconsts.YTDLPFailure, err)
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
func (d *VideoDownload) scanVideoCmdOutput(r io.Reader, filenameChan chan<- string) {
	if d.Video.URL == "" {
		logging.I("Video URL received blank")
		return
	}
	scanner := bufio.NewScanner(r)

	lastUpdate := models.StatusUpdate{
		VideoID:  d.Video.ID,
		VideoURL: d.Video.URL,
		Status:   consts.DLStatusPending,
		Percent:  0.0,
		Error:    nil,
	}

	// Initialize as pending
	d.DLTracker.updates <- lastUpdate

	for scanner.Scan() {
		line := scanner.Text()

		// Get or create state for this video
		state, exists := states[d.Video.URL]
		if !exists {
			state = &VideoDownloadState{URL: d.Video.URL}
			states[d.Video.URL] = state
		}

		switch d.DLTracker.downloader {
		case consts.DownloaderAria:
			newTotal, newCompleted, pct, err := downloaders.Aria2OutputParser(line, state.URL, state.TotalFrags, state.CompletedFrags)
			if err != nil {
				logging.E(0, "Could not parse Aria2 output line %q: %v", line, err)
			}
			// Update state
			state.TotalFrags = newTotal
			state.CompletedFrags = newCompleted

			// Rest of status update logic
			if pct > 0.0 {
				newUpdate := models.StatusUpdate{
					VideoID:  d.Video.ID,
					VideoURL: d.Video.URL,
					Status:   consts.DLStatusDownloading,
					Percent:  pct,
					Error:    d.Video.DownloadStatus.Error,
				}

				if pct == 100.0 {
					newUpdate.Status = consts.DLStatusCompleted
					// Remove completed state
					delete(states, d.Video.URL)
				}

				if newUpdate != lastUpdate {
					d.Video.DownloadStatus.Status = newUpdate.Status
					d.Video.DownloadStatus.Pct = newUpdate.Percent
					d.DLTracker.sendUpdate(d.Video)
					lastUpdate = newUpdate
				}
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
