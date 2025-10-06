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
	"sync"
	"syscall"
	"time"

	"tubarr/internal/cfg"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/downloads/downloaders"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

type VideoDownloadState struct {
	TotalFrags     int
	CompletedFrags int
	URL            string
	Percentage     float64
	Status         consts.DownloadStatus
}

var states sync.Map

// buildVideoCommand builds the command to download a video using yt-dlp.
func (d *VideoDownload) buildVideoCommand() *exec.Cmd {
	args := make([]string, 0, 32)

	// Restrict filenames
	args = append(args, command.RestrictFilenames)

	// Uses JSON file (e.g. 'filename.json') to set 'filename.%(ext)s'
	var outputSyntax string
	if d.Video.JSONCustomFile != "" {
		JSONFileName := filepath.Base(d.Video.JSONCustomFile)
		outputSyntax = strings.TrimSuffix(JSONFileName, ".json") + ".%(ext)s"
	} else {
		outputSyntax = command.FilenameSyntax
	}

	// Output location + restricted filename syntax
	args = append(args,
		command.Output,
		filepath.Join(d.Video.ParsedVideoDir, outputSyntax))

	// Print filename to console upon completion
	args = append(args, command.Print, command.AfterMove)

	// Cookie path
	if d.ChannelURL.CookiePath == "" {
		if d.Video.Settings.CookieSource != "" {
			args = append(args, command.CookiesFromBrowser, d.Video.Settings.CookieSource)
		}
	} else {
		args = append(args, command.CookiePath, d.ChannelURL.CookiePath)
	}

	// Cookie source
	if cfg.IsSet(keys.CookieSource) {
		browserCookieSource := cfg.GetString(keys.CookieSource)
		logging.I("Using cookies from browser %q", browserCookieSource)
		args = append(args, command.CookiesFromBrowser, browserCookieSource)
	} else {
		logging.D(1, "No browser cookies set for channel %q and URL %q, skipping cookies in video download", d.Channel.Name, d.Video.URL)
	}

	// Max filesize specified
	if d.Video.Settings.MaxFilesize != "" {
		args = append(args, command.MaxFilesize, d.Video.Settings.MaxFilesize)
	}

	// External downloaders & arguments
	if d.Video.Settings.ExternalDownloader != "" {
		args = append(args, command.ExternalDLer, d.Video.Settings.ExternalDownloader)
		if d.Video.Settings.ExternalDownloaderArgs != "" {

			switch d.Video.Settings.ExternalDownloader {
			case command.DownloaderAria:

				ariaCmd := command.DownloaderAria + ":" +
					d.Video.Settings.ExternalDownloaderArgs +
					" " +
					command.AriaLog +
					" " +
					command.AriaNoRPC +
					" " +
					command.AriaNoColor +
					" " +
					command.AriaShowConsole +
					" " +
					command.AriaInterval

				args = append(args, command.ExternalDLArgs, ariaCmd)
			default:
				args = append(args, command.ExternalDLArgs, d.Video.Settings.ExternalDownloaderArgs)
			}
		}
	}

	// Retry download X times
	if d.Video.Settings.Retries != 0 {
		args = append(args, command.Retries, strconv.Itoa(d.Video.Settings.Retries))
	}

	// Merge output formats to extension if set
	if d.Video.Settings.YtdlpOutputExt != "" {
		args = append(args, command.YtDLPOutputExtension, d.Video.Settings.YtdlpOutputExt)
	}

	// Randomize requests (avoid detection as bot)
	args = append(args, command.RandomizeRequests...)

	// Add target URL [ MUST GO LAST !! ]
	if d.Video.DirectVideoURL != "" {
		args = append(args, d.Video.DirectVideoURL)
	} else {
		args = append(args, d.Video.URL)
	}

	// Combine command...
	cmd := exec.CommandContext(d.Context, command.YTDLP, args...)
	logging.D(1, "Built video download command for URL %q:\n%v", d.Video.URL, cmd.String())

	return cmd
}

// executeVideoDownload executes the video download command and waits for completion.
func (d *VideoDownload) executeVideoDownload(cmd *exec.Cmd) error {
	if cmd == nil {
		return fmt.Errorf("no command built for URL %s", d.Video.URL)
	}

	// Set process group to allow killing children processes (e.g. Aria2c)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Set pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe error: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe error: %w", err)
	}

	// Set channels
	lineChan := make(chan string, 100)
	filenameChan := make(chan string, 1)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Merge stdout and stderr into lineChan
	go func() {
		scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
		for scanner.Scan() {
			select {
			case lineChan <- scanner.Text():
			case <-d.Context.Done():
				return
			}
		}
	}()

	// Start parser
	go d.scanVideoCmdOutput(lineChan, filenameChan)

	// Wait for completion or cancel
	select {
	case <-d.Context.Done():
		// End the command
		if err := cmd.Cancel(); err != nil {
			if err = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
				logging.E("Failed to kill process %v: %v", cmd.Process.Pid, err)
			}
		}
		return d.Context.Err()

	case filename := <-filenameChan:
		if filename == "" {
			return errors.New("no output filename captured")
		}
		d.Video.VideoPath = filename
	}

	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("yt-dlp failed: %w", err)
	}

	// Ensure file is fully written
	if err := d.waitForFile(d.Video.VideoPath, 10*time.Second); err != nil {
		return err
	}
	if err := verifyVideoDownload(d.Video.VideoPath); err != nil {
		return err
	}

	return nil
}

// scanVideoCmdOutput scans the yt-dlp video download output for relevant information.
func (d *VideoDownload) scanVideoCmdOutput(lineChan <-chan string, filenameChan chan<- string) {
	defer close(filenameChan)

	// Try to load existing state
	val, ok := states.Load(d.Video.URL)
	var state *VideoDownloadState
	if ok {
		state = val.(*VideoDownloadState)
	} else {
		state = &VideoDownloadState{
			URL:        d.Video.URL,
			Percentage: d.Video.DownloadStatus.Pct,
			Status:     d.Video.DownloadStatus.Status,
		}
		states.Store(d.Video.URL, state)
	}

	lastUpdate := models.StatusUpdate{
		VideoID:  d.Video.ID,
		VideoURL: d.Video.URL,
		Status:   state.Status,
		Percent:  state.Percentage,
		Error:    nil,
	}

	var (
		totalItemsFound, totalDownloadedItems int
		completed                             bool
	)

	for line := range lineChan {
		if line != "" {
			logging.D(4, "Video %d download terminal output: %q", d.Video.ID, line)
		}

		// Aria2 progress parsing
		if d.DLTracker.downloader == command.DownloaderAria {
			gotLine, itemsFound, downloadedItems, pct, status :=
				downloaders.Aria2OutputParser(line, state.URL, totalItemsFound, totalDownloadedItems, state.Percentage, state.Status)

			totalItemsFound = itemsFound
			totalDownloadedItems = downloadedItems

			if gotLine {
				state.Percentage = pct
				state.Status = status

				newUpdate := models.StatusUpdate{
					VideoID:  d.Video.ID,
					VideoURL: d.Video.URL,
					Status:   state.Status,
					Percent:  state.Percentage,
					Error:    d.Video.DownloadStatus.Error,
				}

				if newUpdate != lastUpdate {
					d.Video.DownloadStatus.Status = newUpdate.Status
					d.Video.DownloadStatus.Pct = newUpdate.Percent
					d.DLTracker.sendUpdate(d.Video)
					lastUpdate = newUpdate
				}
			}
		}

		// Detect completed filename
		if !completed && strings.HasPrefix(line, "/") {
			ext := filepath.Ext(line)
			for _, validExt := range consts.AllVidExtensions {
				if ext == validExt {
					state.Status = consts.DLStatusCompleted
					state.Percentage = 100.0
					d.Video.DownloadStatus.Status = consts.DLStatusCompleted
					d.Video.DownloadStatus.Pct = 100.0
					d.DLTracker.sendUpdate(d.Video)

					// Safe delete
					states.Delete(d.Video.URL)

					filenameChan <- line
					completed = true
					break
				}
			}
		}
	}
}
