// Package app contains core application functionality.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/downloads"
	"tubarr/internal/file"
	"tubarr/internal/metadata"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/scraper"
	"tubarr/internal/state"
	"tubarr/internal/times"

	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// InitProcess begins processing metadata/videos and respective downloads.
func InitProcess(
	ctx context.Context,
	s contracts.Store,
	c *models.Channel,
	cu *models.ChannelURL,
	videos []*models.Video,
	scrape *scraper.Scraper) (nSucceeded int, nDownloaded int, err error) {
	AddRunningProcess()
	defer SubtractRunningProcess()

	// Initialize variables.
	var (
		errs []error
		conc = sharedvalidation.ValidateConcurrencyLimit(c.ChanSettings.Concurrency)
	)

	// Metarr and download status tracking outlive the job, but not shutdown.
	jobParent := state.JobParent(ctx)
	metarrCtx, metarrCancel := context.WithCancel(jobParent)

	// Running Metarr processes finish in the background, so the channel isn't held until they do.
	var metarrWg sync.WaitGroup
	AddRunningProcess()
	defer func() {
		go func() {
			metarrWg.Wait()
			metarrCancel()
			SubtractRunningProcess()
		}()
	}()

	// Queued videos don't start once the job is stopped, in-progress ones only stop on abort.
	queueCtx := state.QueueCtx(ctx)
	stopRequested := state.StopRequested(ctx)

	// Bot detection context for video workers only.
	procCtx, procCancel := context.WithCancel(ctx)
	defer procCancel()

	// Begin main process.
	logger.Pl.I("Starting meta/video processing for %d videos", len(videos))

	dlTracker := downloads.NewDownloadTracker(s.DownloadStore())
	dlTracker.Start(jobParent)
	defer dlTracker.Stop()

	jobs := make(chan *models.Video, len(videos))
	results := make(chan error, len(videos))

	// Parse directories.
	dirParser := parsing.NewDirectoryParser(c, parsing.TubarrTags)
	if c.ChanSettings.VideoDir, err = dirParser.ParseDirectory(c.ChanSettings.VideoDir, "channel video directory"); err != nil {
		return 0, 0, err
	}
	if c.ChanSettings.JSONDir, err = dirParser.ParseDirectory(c.ChanSettings.JSONDir, "channel JSON directory"); err != nil {
		return 0, 0, err
	}
	if cu.ChanURLSettings.VideoDir, err = dirParser.ParseDirectory(cu.ChanURLSettings.VideoDir, "channel URL video directory"); err != nil {
		return 0, 0, err
	}
	if cu.ChanURLSettings.JSONDir, err = dirParser.ParseDirectory(cu.ChanURLSettings.JSONDir, "channel URL JSON directory"); err != nil {
		return 0, 0, err
	}

	// Check if Metarr exists and can be run.
	metarrErr := metarrExecutable(ctx)
	metarrIsExecutable := metarrErr == nil
	if metarrErr != nil {
		logger.Pl.E("Cannot run 'metarr' due to error: %v", metarrErr)
	}

	// Start workers.
	var (
		wg         sync.WaitGroup
		downloaded atomic.Int32
	)
	for range conc {
		wg.Go(func() {
			for v := range jobs {
				select {
				case <-queueCtx.Done():
					results <- fmt.Errorf("skipped %q: %w", v.URL, queueCtx.Err())
					continue
				default:
				}

				err := videoJob(
					queueCtx,
					procCtx,
					metarrCtx,
					stopRequested,
					s.VideoStore(),
					s.ChannelStore(),
					dlTracker,
					dirParser,
					v,
					cu,
					c,
					&metarrWg,
					metarrIsExecutable,
					&downloaded)

				results <- err
			}
		})
	}

	// Send jobs.
	for _, video := range videos {
		if video == nil {
			logger.Pl.E("Video in queue for channel %q is nil", c.Name)
			continue
		}

		// Get video paths.
		video.JSONDir = cu.ChanURLSettings.JSONDir
		video.VideoDir = cu.ChanURLSettings.VideoDir

		// Use custom scraper if needed.
		if video.JSONFilePath, err = executeCustomScraper(queueCtx, scrape, video); err != nil {
			logger.Pl.E("Custom scraper failed for %q: %v", video.URL, err)
			errs = append(errs, err)
			continue
		}

		// Skip already downloaded videos.
		if video.Finished {
			logger.Pl.D(1, "Video in channel %q with URL %q is already marked as downloaded", c.Name, video.URL)
			continue
		}
		jobs <- video
	}
	close(jobs)

	// Close results after all workers are done.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results.
	for err := range results {
		if err != nil {
			logger.Pl.E("Video processing failed: %v", err)
			errs = append(errs, err)
		} else {
			nSucceeded++
		}
	}

	nDownloaded = int(downloaded.Load())

	// Return errors, and succeeded/downloaded counts.
	if len(errs) > 0 {
		joined := errors.Join(errs...)
		logger.Pl.E("Finished with %d error(s): %v", len(errs), joined)
		return nSucceeded, nDownloaded, joined
	}
	return nSucceeded, nDownloaded, nil
}

// executeCustomScraper checks if a custom scraper should be used for this release.
func executeCustomScraper(ctx context.Context, s *scraper.Scraper, v *models.Video) (customJSON string, err error) {
	if v == nil || s == nil {
		return "", fmt.Errorf("invalid nil parameter (video: %v, scraper: %v)", v == nil, s == nil)
	}

	// Detect custom site.
	if err := s.ScrapeCustomSite(ctx, v.URL, v.JSONDir, v); err != nil {
		return "", err
	}

	return v.JSONCustomFile, nil
}

// videoJob starts a worker's process for a video.
func videoJob(
	queueCtx context.Context,
	procCtx context.Context,
	metarrCtx context.Context,
	stopRequested <-chan struct{},
	vs contracts.VideoStore,
	cs contracts.ChannelStore,
	dlTracker *downloads.DownloadTracker,
	dirParser *parsing.DirectoryParser,
	v *models.Video,
	cu *models.ChannelURL,
	c *models.Channel,
	metarrWg *sync.WaitGroup,
	metarrIsExecutable bool,
	downloaded *atomic.Int32,
) (err error) {
	// Take hold of video before touching its files, cancelling any separate Metarr run on it.
	key := metarrKey{c.ID, v.URL}
	vMetarrCtx, vMetarrCancel := context.WithCancel(metarrCtx)
	claim := &metarrClaim{channelName: c.Name, cancel: vMetarrCancel, pipeline: true, done: make(chan struct{})}
	held, err := preemptMetarr(queueCtx, key, claim)
	if err != nil {
		vMetarrCancel()
		return err
	}
	if !held {
		vMetarrCancel()
		logger.Pl.W("Video %q is already being downloaded elsewhere in channel %q, skipping", v.URL, c.Name)
		return nil
	}
	handedOff := false
	defer func() {
		if !handedOff {
			releaseMetarr(key, claim)
			vMetarrCancel()
		}
	}()

	// Per-video context, canceled from the Downloads page once the video is tracked there.
	vidCtx, vidCancel := context.WithCancelCause(procCtx)
	defer vidCancel(nil)
	tracked := false
	track := func(stage string) {
		if v.ID == 0 {
			return
		}
		if !tracked {
			downloads.RegisterDownloadContext(v.ID, v.URL, vidCancel)
			tracked = true
		}
		dlTracker.SetStatus(v, consts.DLStatusQueued, 0, stage, nil)
	}
	defer func() {
		if !tracked {
			return
		}
		downloads.UnregisterDownloadContext(v.ID, v.URL)
		finishTracking(vidCtx, dlTracker, v, err)
	}()
	track("Waiting to start")

	// Bot detection avoidance wait time.
	waitCtx, waitCancel := context.WithCancelCause(queueCtx)
	stopWait := context.AfterFunc(vidCtx, func() { waitCancel(context.Cause(vidCtx)) })
	err = times.WaitTime(waitCtx, times.RandomSecsDuration(consts.DefaultBotAvoidanceSeconds), c.Name, v.URL)
	stopWait()
	waitCancel(nil)
	if err != nil {
		return err
	}

	// Process JSON metadata phase.
	track("Fetching metadata")
	proceed, botBlockChannel, err := processJSON(vidCtx, vs, dlTracker, dirParser, c, cu, v)
	if errors.Is(err, downloads.ErrDuplicateDownload) {
		logger.Pl.I("Skipping video %q: %v", v.URL, err)
		return nil
	}
	if err != nil {
		handleBotBlock(cs, cu, v.URL, botBlockChannel)
		return fmt.Errorf("metadata processing error for video URL %q: %w", v.URL, err)
	}
	if !proceed {
		logger.Pl.I("Skipping further processing for ignored video: %s", v.URL)
		return nil
	}

	// New videos have an ID once their metadata is stored.
	track("Waiting for download slot")

	// Check move operations.
	v.MoveOpOutputDir = metadata.HandleMoveOps(v, cu, dirParser)

	// Process video download phase.
	botBlockChannel, err = processVideo(vidCtx, v, cu, c, dlTracker)
	if errors.Is(err, downloads.ErrDuplicateDownload) {
		logger.Pl.I("Skipping video %q: %v", v.URL, err)
		return nil
	}
	if err != nil {
		handleBotBlock(cs, cu, v.URL, botBlockChannel)
		return fmt.Errorf("video processing error for video URL %q: %w", v.URL, err)
	}
	downloaded.Add(1)

	// Check if Metarr can be run.
	if !metarrIsExecutable {
		logger.Pl.I("No 'metarr' at $PATH, skipping Metarr process and marking video as finished (Metarr can be run later)")
		v.MetarrStatus = consts.MetarrStatusPending
		return completeAndStoreVideo(cs, vs, v, c)
	}

	// Run metarr step.
	handedOff = true
	metarrWg.Add(1)
	go func(video *models.Video, channelURL *models.ChannelURL, channel *models.Channel) {
		defer metarrWg.Done()
		defer releaseMetarr(key, claim)
		defer vMetarrCancel()
		claim.enqueue(video, channelURL)

		// Not started when the job or Metarr is stopped, mark pending for a later Metarr run.
		storePending := func() {
			logger.Pl.W("Metarr not started for video %s, marking as pending", video.URL)
			video.MetarrStatus = consts.MetarrStatusPending
			if err := completeAndStoreVideo(cs, vs, video, channel); err != nil {
				logger.Pl.E("Failed to store video %q after Metarr cancellation: %v", video.URL, err)
			}
		}

		// Acquire channel and global Metarr slots.
		release, ok := acquireMetarrSlots(vMetarrCtx, stopRequested, channel, channelURL)
		if !ok {
			storePending()
			return
		}
		defer release()
		if vMetarrCtx.Err() != nil || isClosed(stopRequested) {
			storePending()
			return
		}
		claim.startedAt.Store(time.Now().UnixMilli())

		video.MetarrStatus = execMetarr(vMetarrCtx, video, channelURL, channel, dirParser)

		if err := completeAndStoreVideo(cs, vs, video, channel); err != nil {
			logger.Pl.E("Failed to complete video %q after metarr: %v", video.URL, err)
		}
	}(v, cu, c)

	return nil
}

// finishTracking sends a video's final status, dropping it from the Downloads page.
//
// A video leaving the pipeline unfinished (still Queued or Downloading) is marked Failed, or Canceled if the user
// canceled it, so it returns to the Available list.
func finishTracking(vidCtx context.Context, dlTracker *downloads.DownloadTracker, v *models.Video, err error) {
	status := v.DownloadStatus.Status
	if status == consts.DLStatusQueued || status == consts.DLStatusDownloading {
		status = consts.DLStatusFailed
		if errors.Is(context.Cause(vidCtx), downloads.ErrCanceledByUser) {
			status = consts.DLStatusCanceled
		}
	}
	dlTracker.SetStatus(v, status, v.DownloadStatus.Percent, "", err)
}

// isClosed reports whether ch is closed, false for a nil channel.
func isClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// completeAndStoreVideo marks a video as complete.
func completeAndStoreVideo(cs contracts.ChannelStore, vs contracts.VideoStore, v *models.Video, c *models.Channel) error {
	v.MarkVideoAsComplete()
	purgeMetaFileIfSet(v)

	// Update video in database.
	if err := vs.UpdateVideo(v, c.ID); err != nil {
		return fmt.Errorf("failed to update video DB entry: %w", err)
	}

	// Add to unseen list and set notification dot.
	if err := cs.AddNewVideoURLs(c.ID, []string{v.URL}); err != nil {
		logger.Pl.E("Failed to update channel %q notification status: %v", c.Name, err)
	}

	return nil
}

// processJSON coordinates JSON download, validation, and database updates.
func processJSON(
	procCtx context.Context,
	vs contracts.VideoStore,
	dlTracker *downloads.DownloadTracker,
	dirParser *parsing.DirectoryParser,
	c *models.Channel,
	cu *models.ChannelURL,
	v *models.Video,
) (proceed, botBlockChannel bool, err error) {
	if v == nil || cu == nil || c == nil {
		return false, false, fmt.Errorf("invalid nil parameter (video: %v, channelURL: %v, channel: %v)", v == nil, cu == nil, c == nil)
	}

	// ONLY download JSON if it's NOT a custom file (custom scraper already created it).
	if v.JSONCustomFile == "" {
		dl, err := downloads.NewJSONDownload(procCtx, v, cu, c, dlTracker.GetStore(), dlTracker, &downloads.Options{
			MaxRetries:       3,
			RetryMaxInterval: 5,
		})
		if err != nil {
			return false, false, err
		}

		if botBlockChannel, err = dl.Execute(); err != nil {
			return false, botBlockChannel, err
		}
	} else {
		logger.Pl.D(1, "Skipping JSON download - using custom scraped file: %s", v.JSONCustomFile)
	}

	// Validate and filter.
	passedChecks, useFilteredMetaOps, useFilteredFilenameOps, err := metadata.ValidateAndFilter(v, cu, c, dirParser)
	if err != nil {
		return false, false, err
	}
	// Store filtered operations in the video (per-video, not shared).
	v.FilteredMetaOps = useFilteredMetaOps
	v.FilteredFilenameOps = useFilteredFilenameOps

	// If video failed checks, mark and save to DB.
	if !passedChecks {
		// Remove metadata JSON file since video is being ignored.
		if v.JSONFilePath != "" {
			if err := file.RemoveMetadataJSON(v.JSONFilePath); err != nil && !os.IsNotExist(err) {
				logger.Pl.W("Failed to remove metadata file for ignored video %q: %v", v.JSONFilePath, err)
			}
		}

		v.MarkVideoAsIgnored()
		if v.ID, err = vs.AddVideo(v, c.ID, cu.ID); err != nil {
			return false, false, fmt.Errorf("failed to update ignored video: %w", err)
		}
		return false, false, nil
	}

	// Will download this video (passed checks).
	v.DownloadStatus.Status = consts.DLStatusQueued
	v.DownloadStatus.Percent = 0.0
	logger.Pl.D(1, "Setting video %q to Queued status before saving to DB", v.URL)

	// Save video to database.
	if v.ID, err = vs.AddVideo(v, c.ID, cu.ID); err != nil {
		return false, false, fmt.Errorf("failed to update video DB entry: %w", err)
	}

	logger.Pl.S("Processed metadata for: %s", v.URL)
	return true, false, nil
}

// processVideo processes video downloads.
func processVideo(vidCtx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, dlTracker *downloads.DownloadTracker) (botBlockChannel bool, err error) {
	if v == nil || cu == nil || c == nil {
		return false, fmt.Errorf("invalid nil parameter (video: %v, channelURL: %v, channel: %v)", v == nil, cu == nil, c == nil)
	}

	logger.Pl.I("Processing video download for URL: %s", v.URL)

	dl, err := downloads.NewVideoDownload(vidCtx, v, cu, c, dlTracker.GetStore(), dlTracker, &downloads.Options{
		MaxRetries:       3,
		RetryMaxInterval: 20,
	})
	if err != nil {
		return false, err
	}

	if botBlockChannel, err = dl.Execute(); err != nil {
		return botBlockChannel, err
	}

	logger.Pl.D(1, "Successfully processed and marked as downloaded: %s", v.URL)
	return false, nil
}

// handleBotBlock handles cases where the program has been detected as a bot.
// videoURL should be the actual video URL that triggered the bot detection (important for manual downloads).
func handleBotBlock(cs contracts.ChannelStore, cu *models.ChannelURL, videoURL string, botBlock bool) {
	if !botBlock {
		return
	}
	if err := blockChannelBotDetected(cs, cu, videoURL); err != nil {
		logger.Pl.E("Failed to block channel: %v", err)
	}
}
