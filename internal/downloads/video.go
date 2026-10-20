package downloads

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"tubarr/internal/domain/command"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/downloads/downloaders"
	"tubarr/internal/file"
	"tubarr/internal/parsing"
	"tubarr/internal/siterules"
	"tubarr/internal/state"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"
)

// buildVideoCommand builds the command to download a video using yt-dlp.
func (d *VideoDownload) buildVideoCommand() *exec.Cmd {
	args := make([]string, 0, 32)

	// Restrict filenames.
	args = append(args, command.RestrictFilenames)

	// Infer video filename from JSON filename, escaping '%' so yt-dlp writes it as named.
	var outputSyntax string
	if d.Video.JSONCustomFile != "" {
		JSONFileName := strings.TrimSuffix(filepath.Base(d.Video.JSONCustomFile), ".json")
		outputSyntax = strings.ReplaceAll(JSONFileName, "%", "%%") + ".%(ext)s"
	} else {
		outputSyntax = command.FilenameSyntax
	}

	// Output location + restricted filename syntax.
	args = append(args, command.P, d.Video.VideoDir)
	args = append(args, command.Output, outputSyntax)

	// Print filename to console upon completion.
	args = append(args, command.Print, command.AfterMove)

	// Machine-readable progress lines.
	args = append(args, command.Progress, command.ProgressNewline, command.ProgressTemplate, command.ProgressTemplateFmt)

	// Cookie path.
	if d.ChannelURL.CookiePath == "" {
		if d.ChannelURL.ChanURLSettings.CookiesFromBrowser != "" {
			args = append(args, command.CookiesFromBrowser, d.ChannelURL.ChanURLSettings.CookiesFromBrowser)
		}
	} else {
		args = append(args, command.CookiePath, d.ChannelURL.CookiePath)
	}

	// Cookie source, for channel URLs without their own.
	if d.ChannelURL.ChanURLSettings.CookiesFromBrowser == "" && abstractions.IsSet(keys.CookiesFromBrowser) {
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
		dlName := downloaders.Name(d.ChannelURL.ChanURLSettings.ExternalDownloader)

		// ffmpeg and wget progress is hidden by yt-dlp, so always request progress output Tubarr can read.
		var progressArgs string
		switch dlName {
		case command.DownloaderFFmpeg:
			progressArgs = command.FFmpegProgress
		case command.DownloaderWget:
			progressArgs = command.WgetProgress
		}

		if progressArgs != "" {
			dlArgs := strings.TrimSpace(d.ChannelURL.ChanURLSettings.ExternalDownloaderArgs + " " + progressArgs)
			args = append(args, command.ExternalDLArgs, dlName+":"+dlArgs)

		} else if d.ChannelURL.ChanURLSettings.ExternalDownloaderArgs != "" {

			switch dlName {
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
		args = append(args, parsing.SplitArgs(d.ChannelURL.ChanURLSettings.ExtraYTDLPVideoArgs)...)
	}
	if abstractions.IsSet(keys.ExtraYTDLPVideoArgs) {
		args = append(args, parsing.SplitArgs(abstractions.GetString(keys.ExtraYTDLPVideoArgs))...)
	}

	// Add preferred codecs.
	switch {
	case len(d.ChannelURL.ChanURLSettings.YTDLPPreferredVideoCodecs) > 0 && len(d.ChannelURL.ChanURLSettings.YTDLPPreferredAudioCodecs) > 0:
		pref := buildVideoCodecList(d.ChannelURL.ChanURLSettings.YTDLPPreferredVideoCodecs) + "+" + buildAudioCodecList(d.ChannelURL.ChanURLSettings.YTDLPPreferredAudioCodecs) + "/b"
		args = append(args, "-f", pref)
	case len(d.ChannelURL.ChanURLSettings.YTDLPPreferredVideoCodecs) > 0:
		pref := buildVideoCodecList(d.ChannelURL.ChanURLSettings.YTDLPPreferredVideoCodecs) + "+ba/b"
		args = append(args, "-f", pref)
	case len(d.ChannelURL.ChanURLSettings.YTDLPPreferredAudioCodecs) > 0:
		pref := buildAudioCodecList(d.ChannelURL.ChanURLSettings.YTDLPPreferredAudioCodecs) + "+bv*/b"
		args = append(args, "-f", pref)
	}

	// Site rule impersonation, and FlareSolverr's user agent (its cookies are in the cookie file).
	args, d.fsGen = siterules.AddSiteRulesToYTDLP(d.Video.URL, d.ChannelURL.FlareSolverr, args)

	// Add target URL [ MUST GO LAST !! ]
	if d.Video.DirectVideoURL != "" {
		args = append(args, command.EndOfOptions, d.Video.DirectVideoURL)
	} else {
		args = append(args, command.EndOfOptions, d.Video.URL)
	}

	// Combine command...
	cmd := exec.CommandContext(d.Context, command.YTDLP, args...)
	useProcessGroup(cmd)
	return cmd
}

// videoParseResult is the outcome read from a video download's output.
type videoParseResult struct {
	filename     string
	destinations []string
	err          error
}

// executeVideoDownload executes the video download command and waits for completion.
func (d *VideoDownload) executeVideoDownload(cmd *exec.Cmd) error {
	if cmd == nil {
		return fmt.Errorf("no command built for URL %s", d.Video.URL)
	}

	// External downloader environment workarounds.
	cleanup, err := d.setDownloaderEnv(cmd)
	if err != nil {
		return err
	}
	defer cleanup()

	// Set pipes.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe error: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe error: %w", err)
	}

	// Hold the video so it can't be downloaded twice at once (locks from other downloads until deletion from map).
	if !state.ClaimActiveVideoDownload(d.Video.ID, d.Video.DownloadStatus) {
		return fmt.Errorf("video with ID %d is already downloading (URL: %q)", d.Video.ID, d.Video.URL)
	}
	defer state.DeleteActiveVideoDownloadStatus(d.Video.ID)

	lineChan := make(chan string, 100)

	logger.Pl.I("Running video download command for video %q (Channel: %q):\n\n%v\n", d.Video.URL, d.Channel.Name, cmd.String())
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start command: %w", err)
	}
	defer killProcessGroup(cmd)

	// Merge stdout and stderr into lineChan, reading both concurrently so neither pipe can fill and stall yt-dlp.
	var readers sync.WaitGroup
	for _, pipe := range []io.Reader{stdout, stderr} {
		readers.Go(func() {
			scanner := bufio.NewScanner(pipe)
			scanner.Split(scanLinesOrCR)
			for scanner.Scan() {
				select {
				case lineChan <- scanner.Text():
				case <-d.Context.Done():
					logger.Pl.W("Download canceled: %v", d.Context.Err())
					return
				}
			}
			if err := scanner.Err(); err != nil {
				logger.Pl.E("Scanner error reading yt-dlp output: %v", err)
			}
		})
	}
	go func() {
		readers.Wait()
		close(lineChan)
	}()

	// Parse output until both pipes close (the process exited or was killed on cancel).
	resultChan := make(chan videoParseResult, 1)
	go func() {
		resultChan <- d.scanVideoCmdOutput(lineChan)
	}()
	result := <-resultChan
	d.destinations = append(d.destinations, result.destinations...)

	// cmd.Wait closes the pipes, so readers must reach EOF first.
	readers.Wait()
	waitErr := cmd.Wait()

	if err := d.Context.Err(); err != nil {
		logger.Pl.W("Download canceled: %v", err)
		return err
	}
	if result.err != nil {
		return result.err
	}
	if waitErr != nil {
		return fmt.Errorf("yt-dlp failed: %w", waitErr)
	}
	if result.filename == "" {
		return errors.New("no output filename captured")
	}
	d.Video.VideoFilePath = result.filename

	// Ensure file is fully written.
	if err := d.waitForFile(d.Video.VideoFilePath, 10*time.Second); err != nil {
		return err
	}

	// Poll filesize.
	if size := file.Size(d.Video.VideoFilePath); size > 0 {
		d.Video.Filesize = size
	}

	return verifyVideoDownload(d.Video.VideoFilePath)
}

// scanVideoCmdOutput scans the yt-dlp video download output for the completed filename or errors.
func (d *VideoDownload) scanVideoCmdOutput(lineChan <-chan string) videoParseResult {
	var (
		progress     = downloaders.NewProgressParser(d.ChannelURL.ChanURLSettings.ExternalDownloader, d.durationSeconds())
		completed    bool
		filename     string
		destinations []string
		errorLines   []string
	)
	for line := range lineChan {
		if line != "" {
			logger.Pl.D(4, "Video %d download terminal output: %q", d.Video.ID, line)
		}

		// Keep draining after completion so the pipe readers can reach EOF before cmd.Wait.
		if completed {
			continue
		}

		// Files being downloaded to, whose partials are removed if the user cancels.
		if dest, ok := strings.CutPrefix(line, command.DownloadDestinationPrefix); ok {
			destinations = append(destinations, strings.TrimSpace(dest))
			continue
		}

		// Collect error messages, and the notice of a file skipped for --max-filesize.
		if strings.HasPrefix(strings.ToLower(line), "error:") || strings.Contains(line, "File is larger than max-filesize") {
			errorLines = append(errorLines, strings.TrimSpace(line))
		}

		// yt-dlp and external downloader progress parsing.
		if pct, ok := progress.Parse(line); ok {
			d.updateProgress(consts.DLStatusDownloading, pct)
			continue
		}

		// Detect completed filename.
		if strings.HasPrefix(line, "/") {
			if _, ok := sharedconsts.AllVidExtensions[filepath.Ext(line)]; ok { // Download succeeded.

				d.DLTracker.SetStatus(d.Video, consts.DLStatusCompleted, 100.0, "", nil)

				filename = line
				completed = true
			}
		}
	}

	result := videoParseResult{destinations: destinations}
	switch {
	case completed:
		result.filename = filename
	case len(errorLines) > 0:
		// Limit to last 5 error lines to avoid overwhelming the error message.
		if len(errorLines) > 5 {
			errorLines = errorLines[len(errorLines)-5:]
		}
		result.err = fmt.Errorf("yt-dlp failed: %s", strings.Join(errorLines, "; "))
	}
	return result
}

// setDownloaderEnv applies environment workarounds for the external downloader, returning a cleanup to run after the command exits.
func (d *VideoDownload) setDownloaderEnv(cmd *exec.Cmd) (cleanup func(), err error) {
	if downloaders.Name(d.ChannelURL.ChanURLSettings.ExternalDownloader) != command.DownloaderHttpie {
		return func() {}, nil
	}
	dir, err := downloaders.HttpieConfigDir()
	if err != nil {
		return nil, err
	}
	cmd.Env = append(cmd.Environ(), command.HttpieConfigDirEnv+"="+dir)
	return func() {
		if err := os.RemoveAll(dir); err != nil {
			logger.Pl.W("Failed to remove temporary httpie config directory %q: %v", dir, err)
		}
	}, nil
}

// durationSeconds returns the video duration from its metadata, falling back to the crawl listing's duration.
func (d *VideoDownload) durationSeconds() float64 {
	switch v := d.Video.MetadataMap["duration"].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	}
	return float64(d.Video.Duration)
}

// updateProgress sends a status update when the status or whole percentage changes.
func (d *VideoDownload) updateProgress(status consts.DownloadStatus, pct float64) {
	prev := d.Video.DownloadStatus
	d.Video.DownloadStatus.Status = status
	d.Video.DownloadStatus.Percent = pct
	if status != prev.Status || math.Floor(pct) != math.Floor(prev.Percent) {
		d.DLTracker.SendUpdate(d.Video)
	}
}

// scanLinesOrCR is a bufio.SplitFunc splitting on "\n", "\r\n", or a lone "\r" (progress bar redraws).
func scanLinesOrCR(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		if data[i] == '\r' && i+1 < len(data) && data[i+1] == '\n' {
			return i + 2, data[:i], nil
		}
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}
