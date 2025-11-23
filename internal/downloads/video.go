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
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/downloads/downloaders"
	"tubarr/internal/state"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"
)

// buildVideoCommand builds the command to download a video using yt-dlp.
func (d *VideoDownload) buildVideoCommand() *exec.Cmd {
	args := make([]string, 0, 32)

	// Restrict filenames.
	args = append(args, command.RestrictFilenames)

	// Infer video filename from JSON filename.
	var outputSyntax string
	if d.Video.JSONCustomFile != "" {
		JSONFileName := strings.TrimSuffix(filepath.Base(d.Video.JSONCustomFile), ".json")
		outputSyntax = JSONFileName + ".%(ext)s"
	} else {
		outputSyntax = command.FilenameSyntax
	}

	// Output location + restricted filename syntax.
	args = append(args, command.P, d.Video.ParsedVideoDir)
	args = append(args, command.Output, outputSyntax)

	// Print filename to console upon completion.
	args = append(args, command.Print, command.AfterMove)

	// Cookie path.
	if d.ChannelURL.CookiePath == "" {
		if d.ChannelURL.ChanURLSettings.CookiesFromBrowser != "" {
			args = append(args, command.CookiesFromBrowser, d.ChannelURL.ChanURLSettings.CookiesFromBrowser)
		}
	} else {
		args = append(args, command.CookiePath, d.ChannelURL.CookiePath)
	}

	// Cookie source.
	if abstractions.IsSet(keys.CookiesFromBrowser) {
		browserCookiesFromBrowser := abstractions.GetString(keys.CookiesFromBrowser)
		logger.Pl.I("Using cookies from browser %q", browserCookiesFromBrowser)
		args = append(args, command.CookiesFromBrowser, browserCookiesFromBrowser)
	} else {
		logger.Pl.D(1, "No browser cookies set for channel %q and URL %q, skipping cookies in video download", d.Channel.Name, d.Video.URL)
	}

	// Max filesize specified.
	if d.ChannelURL.ChanURLSettings.MaxFilesize != "" {
		args = append(args, command.MaxFilesize, d.ChannelURL.ChanURLSettings.MaxFilesize)
	}

	// External downloaders and arguments.
	if d.ChannelURL.ChanURLSettings.ExternalDownloader != "" {
		args = append(args, command.ExternalDLer, d.ChannelURL.ChanURLSettings.ExternalDownloader)
		if d.ChannelURL.ChanURLSettings.ExternalDownloaderArgs != "" {

			switch d.ChannelURL.ChanURLSettings.ExternalDownloader {
			case command.DownloaderAria:

				ariaCmd := command.DownloaderAria + ":" +
					d.ChannelURL.ChanURLSettings.ExternalDownloaderArgs +
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
				args = append(args, command.ExternalDLArgs, d.ChannelURL.ChanURLSettings.ExternalDownloaderArgs)
			}
		}
	}

	// Retry download X times.
	if d.ChannelURL.ChanURLSettings.Retries != 0 {
		args = append(args, command.Retries, strconv.Itoa(d.ChannelURL.ChanURLSettings.Retries))
	}

	// Merge output formats to extension if set.
	if d.ChannelURL.ChanURLSettings.YtdlpOutputExt != "" {
		args = append(args, command.YtDLPOutputExtension, d.ChannelURL.ChanURLSettings.YtdlpOutputExt)
	}

	// Randomize requests (avoid detection as bot).
	args = append(args, command.SleepRequests...)

	// Additional user arguments.
	if !abstractions.IsSet(keys.ExtraYTDLPVideoArgs) && d.ChannelURL.ChanURLSettings.ExtraYTDLPVideoArgs != "" {
		args = append(args, strings.Fields(d.ChannelURL.ChanURLSettings.ExtraYTDLPVideoArgs)...)
	}
	if abstractions.IsSet(keys.ExtraYTDLPVideoArgs) {
		args = append(args, strings.Fields(abstractions.GetString(keys.ExtraYTDLPVideoArgs))...)
	}

	// Add target URL [ MUST GO LAST !! ]
	if d.Video.DirectVideoURL != "" {
		args = append(args, d.Video.DirectVideoURL)
	} else {
		args = append(args, d.Video.URL)
	}

	// Combine command...
	cmd := exec.CommandContext(d.Context, command.YTDLP, args...)
	return cmd
}

// executeVideoDownload executes the video download command and waits for completion.
func (d *VideoDownload) executeVideoDownload(cmd *exec.Cmd) error {
	if cmd == nil {
		return fmt.Errorf("no command built for URL %s", d.Video.URL)
	}

	// Set pipes.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe error: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe error: %w", err)
	}

	// Set channels.
	lineChan := make(chan string, 100)
	filenameChan := make(chan string, 1)
	errChan := make(chan error, 1)

	logger.Pl.I("Running video download command for video %q (Channel: %q):\n\n%v\n", d.Video.URL, d.Channel.Name, cmd.String())
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}

	// Merge stdout and stderr into lineChan.
	go func() {
		defer close(lineChan)
		scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
		for scanner.Scan() {
			select {
			case lineChan <- scanner.Text():
			case <-d.Context.Done():
				logger.Pl.W("Download cancelled: %v", d.Context.Err())
				return
			}
		}
	}()

	// Start parser.
	go d.scanVideoCmdOutput(lineChan, filenameChan, errChan)

	// Wait for completion, error, or cancel.
	var filename string
	var parseErr error

	select {
	case <-d.Context.Done():
		logger.Pl.W("Download cancelled: %v", d.Context.Err())

		// End the command.
		if err := cmd.Cancel(); err != nil {
			if cmd.Process != nil {
				if err := cmd.Process.Kill(); err != nil {
					logger.Pl.E("Failed to kill video download process %v: %v", cmd.Process, err)
				}
			}
		}
		return d.Context.Err()

	case parseErr = <-errChan:
		// Error detected by parser.
		if parseErr != nil {
			return parseErr
		}

	case filename = <-filenameChan:
		if filename == "" {
			return errors.New("no output filename captured")
		}
		d.Video.VideoPath = filename
	}

	// Wait for the command to finish.
	if err := cmd.Wait(); err != nil {
		// Return parse error if present.
		if parseErr != nil {
			return parseErr
		}
		return fmt.Errorf("yt-dlp failed: %w", err)
	}

	// Verify filename.
	if filename != "" {
		// Ensure file is fully written.
		if err := d.waitForFile(d.Video.VideoPath, 10*time.Second); err != nil {
			return err
		}
		if err := verifyVideoDownload(d.Video.VideoPath); err != nil {
			return err
		}
	}
	return nil
}

// scanVideoCmdOutput scans the yt-dlp video download output for relevant information.
func (d *VideoDownload) scanVideoCmdOutput(lineChan <-chan string, filenameChan chan<- string, errChan chan<- error) {
	defer close(filenameChan)
	defer close(errChan)

	// Check if video is already downloading.
	if state.ActiveVideoDownloadStatusExists(d.Video.ID) {
		errChan <- fmt.Errorf("video with ID %d is already downloading (URL: %q)", d.Video.ID, d.Video.URL)
		return
	}

	// Save state to map (locks from other downloads until deletion from map).
	state.SetActiveVideoDownloadStatus(d.Video.ID, d.Video.DownloadStatus)
	defer state.DeleteActiveVideoDownloadStatus(d.Video.ID)

	var (
		totalItemsFound, totalDownloadedItems int
		completed                             bool
		errorLines                            []string
	)
	for line := range lineChan {
		if line != "" {
			logger.Pl.D(4, "Video %d download terminal output: %q", d.Video.ID, line)
		}

		// Collect error messages.
		lowerLine := strings.ToLower(line)
		if strings.HasPrefix(lowerLine, "error:") {

			if strings.Contains(lowerLine, "forbidden") ||
				strings.Contains(lowerLine, "error 403") ||
				strings.Contains(lowerLine, "error 404") ||
				strings.Contains(lowerLine, "unable to download") ||
				strings.Contains(lowerLine, "http error") {
				errorLines = append(errorLines, strings.TrimSpace(line))
			}
		}

		// Aria2 progress parsing.
		if d.DLTracker.downloader == command.DownloaderAria {
			gotLine, itemsFound, downloadedItems, pct, status :=
				downloaders.Aria2OutputParser(line, totalItemsFound, totalDownloadedItems, d.Video.DownloadStatus.Percent, d.Video.DownloadStatus.Status)

			totalItemsFound = itemsFound
			totalDownloadedItems = downloadedItems

			if gotLine {
				d.Video.DownloadStatus.Status = status
				d.Video.DownloadStatus.Percent = pct
				d.DLTracker.sendUpdate(d.Video)
			}
		}

		// Detect completed filename.
		if !completed && strings.HasPrefix(line, "/") {
			if _, ok := sharedconsts.AllVidExtensions[filepath.Ext(line)]; ok { // Download succeeded.

				// Set model to complete.
				d.Video.DownloadStatus.Status = consts.DLStatusCompleted
				d.Video.DownloadStatus.Percent = 100.0
				d.DLTracker.sendUpdate(d.Video)

				filenameChan <- line
				completed = true
				break
			}
		}
	}

	// Send errors if download failed.
	if !completed && len(errorLines) > 0 {
		// Limit to last 5 error lines to avoid overwhelming the error message.
		if len(errorLines) > 5 {
			errorLines = errorLines[len(errorLines)-5:]
		}
		errMsg := fmt.Sprintf("yt-dlp failed: %s", strings.Join(errorLines, "; "))
		errChan <- errors.New(errMsg)
	}
}
