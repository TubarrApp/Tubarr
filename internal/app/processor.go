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
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// videoBatch holds what the video jobs of one channel URL's batch share.
type videoBatch struct {
	stopRequested <-chan struct{}
	vs            contracts.VideoStore
	cs            contracts.ChannelStore
	dlTracker     *downloads.DownloadTracker
	dirParser     *parsing.DirectoryParser
	cu            *models.ChannelURL
	c             *models.Channel
	metarrWg      *sync.WaitGroup // Running Metarr processes outlive the batch.
	downloaded    *atomic.Int32
}

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

	// Initialize variables. The URL's download limit is the channel's unless it sets its own.
	var errs []error
	limit := c.ChanSettings.Concurrency
	if cu.ChanURLSettings != nil && cu.ChanURLSettings.Concurrency > 0 {
		limit = cu.ChanURLSettings.Concurrency
	}
	conc := sharedvalidation.ValidateConcurrencyLimit(limit)

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
	dirParser := parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)
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

	// An unusable filter file prevents unintentional unwanted downloads.
	var skipDownloads models.SkipDownloads
	if _, filterErr := metadata.LoadFilterOpsFromFile(cu, dirParser); filterErr != nil {
		logger.Pl.E("Not downloading crawled videos for %q until its filter file is fixed: %v", cu.URL, filterErr)
		errs = append(errs, fmt.Errorf("channel URL %q: %w", cu.URL, filterErr))
		skipDownloads = models.SkipDownloads{Skip: true, Reason: models.SkipDLMissingFilterFile}
	}

	// Metarr is held back if it can't run or an ops file can't be used, leaving videos pending for Run Metarr.
	skipMetarr, skipErr := metarrSkip(metarrExecutable(ctx), cu, dirParser)
	if skipErr != nil {
		logger.Pl.E("Not running Metarr for %q (videos stay pending for Run Metarr): %s: %v", cu.URL, skipMetarr.Reason, skipErr)
	}

	// Start workers.
	var (
		wg         sync.WaitGroup
		downloaded atomic.Int32
	)
	batch := &videoBatch{
		stopRequested: stopRequested,
		vs:            s.VideoStore(),
		cs:            s.ChannelStore(),
		dlTracker:     dlTracker,
		dirParser:     dirParser,
		cu:            cu,
		c:             c,
		metarrWg:      &metarrWg,
		downloaded:    &downloaded,
	}
	for range conc {
		wg.Go(func() {
			for v := range jobs {
				select {
				case <-queueCtx.Done():
					results <- fmt.Errorf("skipped %q: %w", v.URL, queueCtx.Err())
					continue
				default:
				}
				results <- batch.videoJob(queueCtx, procCtx, metarrCtx, v)
			}
		})
	}

	// Send jobs.
	sent := make([]*models.Video, 0, len(videos))
	for _, video := range videos {
		if video == nil {
			logger.Pl.E("Video in queue for channel %q is nil", c.Name)
			continue
		}

		// Chosen videos skip download filters, so only crawled ones wait for a usable filter file.
		if !video.SkipFilters && !cu.IsManual {
			video.SkipDownloads = skipDownloads
		}
		if video.SkipDownloads.Skip {
			logger.Pl.W("Skipping download of %q: %s", video.URL, video.SkipDownloads.Reason)
			continue
		}
		// Download As videos don't use the ops files, so only Metarr being unusable holds them back.
		if video.Overrides == nil || skipMetarr.Reason == models.SkipMetarrNoPath {
			video.SkipMetarr = skipMetarr
		}

		// Get video paths.
		video.JSONDir = cu.ChanURLSettings.JSONDir
		video.VideoDir = cu.ChanURLSettings.VideoDir
		if o := video.Overrides; o != nil {
			if video.VideoDir, err = overrideDir(dirParser, o.VideoDir, video.VideoDir, "Download As video directory"); err == nil {
				video.JSONDir, err = overrideDir(dirParser, o.JSONDir, video.JSONDir, "Download As JSON directory")
			}
			if err != nil {
				logger.Pl.E("Skipping download of %q: %v", video.URL, err)
				recordSkip(s.VideoStore(), c, video.URL, err)
				errs = append(errs, err)
				continue
			}
		}

		// Use custom scraper if needed.
		if video.JSONFilePath, err = executeCustomScraper(queueCtx, scrape, video); err != nil {
			logger.Pl.E("Custom scraper failed for %q: %v", video.URL, err)
			recordSkip(s.VideoStore(), c, video.URL, fmt.Errorf("custom scraper failed: %w", err))
			errs = append(errs, err)
			continue
		}

		// Skip already downloaded videos.
		if video.Finished {
			logger.Pl.D(1, "Video in channel %q with URL %q is already marked as downloaded", c.Name, video.URL)
			continue
		}

		// New videos are stored now, so they show on the Downloads page while waiting for a worker.
		if video.ID == 0 {
			video.DownloadStatus = models.DLStatus{Status: consts.DLStatusQueued}
			if video.ID, err = s.VideoStore().AddVideo(video, c.ID, cu.ID); err != nil {
				logger.Pl.E("Skipping download of %q: failed to store it: %v", video.URL, err)
				errs = append(errs, err)
				continue
			}
		}
		downloads.Enqueue(video, "Waiting to start")
		sent = append(sent, video)
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

	// Videos the workers never started (the job was stopped) go to the Available list, canceled if the user chose them.
	chosen, crawled := make([]*models.Video, 0, len(sent)), make([]*models.Video, 0, len(sent))
	for _, v := range sent {
		if v.SkipFilters || cu.IsManual {
			chosen = append(chosen, v)
		} else {
			crawled = append(crawled, v)
		}
	}
	if err := finishQueued(s.VideoStore(), c, chosen, consts.DLStatusCanceled, ""); err != nil {
		errs = append(errs, err)
	}
	if err := finishQueued(s.VideoStore(), c, crawled, consts.DLStatusFailed, "the queue was stopped before this video started"); err != nil {
		errs = append(errs, err)
	}

	// Return errors, and succeeded/downloaded counts.
	if len(errs) > 0 {
		joined := errors.Join(errs...)
		logger.Pl.E("Finished with %d error(s): %v", len(errs), joined)
		return nSucceeded, nDownloaded, joined
	}
	return nSucceeded, nDownloaded, nil
}

// overrideDir returns a Download As directory filled in, or dir if override is blank.
func overrideDir(dp *parsing.DirectoryParser, override, dir, name string) (string, error) {
	if override == "" {
		return dir, nil
	}
	// Don't download anywhere unexpected (e.g. onto the local disk under an unmounted drive's mount point).
	if err := validation.CheckDestination(override); err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return dp.ParseDirectory(override, name)
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
func (b *videoBatch) videoJob(queueCtx, procCtx, metarrCtx context.Context, v *models.Video) (err error) {
	c, cu := b.c, b.cu

	// Canceled from the Downloads page while it waited in the queue.
	queuedCtx, canceled := downloads.Start(v)
	defer downloads.Dequeue(v)
	if canceled {
		logger.Pl.I("Skipping video %q: canceled before it started", v.URL)
		return nil
	}

	// Take hold of video before touching its files, canceling any separate Metarr run on it.
	// The hold stops a Run Metarr re-run and this download using the video's files at the same time.
	key := metarrKey{c.ID, v.URL}

	// This video's own Metarr context, so Stop on the Post-processing page can cancel just its run.
	vMetarrCtx, vMetarrCancel := context.WithCancel(metarrCtx)

	// pipeline marks the hold as a download's, which other downloads can't take over.
	claim := &metarrClaim{channelName: c.Name, cancel: vMetarrCancel, pipeline: true, done: make(chan struct{})}
	held, err := preemptMetarr(queueCtx, key, claim)

	// Errors if the job is stopped while waiting for the canceled Metarr run to finish.
	if err != nil {
		vMetarrCancel()
		return err
	}
	// Not held: another download of this video in the channel already has it.
	if !held {
		vMetarrCancel()
		logger.Pl.W("Video %q is already being downloaded elsewhere in channel %q, skipping", v.URL, c.Name)
		return nil
	}
	// Released here on return until Metarr starts, then by the Metarr goroutine once it finishes.
	handedOff := false
	defer func() {
		if !handedOff {
			releaseMetarr(key, claim)
			vMetarrCancel()
		}
	}()

	// Per-video context, canceled from the Downloads page.
	vidCtx, vidCancel := context.WithCancelCause(procCtx)
	defer vidCancel(nil)
	stopCancelLink := context.AfterFunc(queuedCtx, func() { vidCancel(context.Cause(queuedCtx)) })
	defer stopCancelLink()
	tracked := false
	track := func(stage string) {
		if v.ID == 0 {
			return
		}
		if !tracked {
			downloads.Tracked(v)
			tracked = true
		}
		b.dlTracker.SetStatus(v, consts.DLStatusQueued, 0, stage, nil)
	}
	defer func() {
		if !tracked {
			return
		}
		finishTracking(vidCtx, b.dlTracker, v, err)
	}()
	track("Waiting to start")

	// Fail fast once an earlier video's bot detection has blocked the domain.
	if err := downloads.CheckIfAvoidURL(v.URL, cu, b.cs.GetDB()); err != nil {
		return err
	}

	// Bot detection avoidance wait time.
	waitCtx, waitCancel := context.WithCancelCause(queueCtx)
	stopWait := context.AfterFunc(vidCtx, func() { waitCancel(context.Cause(vidCtx)) })
	err = times.WaitTime(waitCtx, times.RandomSecsDuration(consts.DefaultBotAvoidanceSeconds), c.Name, "", v.URL)
	stopWait()
	waitCancel(nil)
	if err != nil {
		return err
	}

	// Process JSON metadata phase.
	track("Fetching metadata")
	proceed, err := processJSON(vidCtx, b.vs, b.dlTracker, b.dirParser, c, cu, v)
	if errors.Is(err, downloads.ErrDuplicateDownload) {
		logger.Pl.I("Skipping video %q: %v", v.URL, err)
		return nil
	}
	if err != nil {
		return fmt.Errorf("metadata processing error for video URL %q: %w", v.URL, err)
	}
	if !proceed {
		logger.Pl.I("Skipping further processing for ignored video: %s", v.URL)
		return nil
	}

	track("Waiting for download slot")

	// Check move operations.
	v.MoveOpOutputDir = metadata.HandleMoveOps(v, cu, b.dirParser)

	// Process video download phase.
	err = processVideo(vidCtx, v, cu, c, b.dlTracker)
	if errors.Is(err, downloads.ErrDuplicateDownload) {
		logger.Pl.I("Skipping video %q: %v", v.URL, err)
		return nil
	}
	if err != nil {
		return fmt.Errorf("video processing error for video URL %q: %w", v.URL, err)
	}
	b.downloaded.Add(1)

	// Metarr held back for the batch: finished with Metarr pending, so Run Metarr can process it once fixed.
	if v.SkipMetarr.Skip {
		logger.Pl.W("Skipping Metarr for %q, marking it finished with Metarr pending: %s", v.URL, v.SkipMetarr.Reason)
		v.MetarrStatus = consts.MetarrStatusPending
		return completeAndStoreVideo(b.cs, b.vs, v, c)
	}

	// Store the downloaded file's path first, so startup can recover the video if Tubarr stops during Metarr.
	if err := b.vs.UpdateVideo(v, c.ID); err != nil {
		logger.Pl.E("Failed to store downloaded file path for video %q before Metarr: %v", v.URL, err)
	}

	// Run metarr step.
	handedOff = true
	b.metarrWg.Add(1)
	go func(video *models.Video, channelURL *models.ChannelURL, channel *models.Channel) {
		defer b.metarrWg.Done()
		defer releaseMetarr(key, claim)
		defer vMetarrCancel()
		claim.enqueue(video, channelURL)

		// Not started when the job or Metarr is stopped, mark pending for a later Metarr run.
		storePending := func() {
			logger.Pl.W("Metarr not started for video %s, marking as pending", video.URL)
			video.MetarrStatus = consts.MetarrStatusPending
			if err := completeAndStoreVideo(b.cs, b.vs, video, channel); err != nil {
				logger.Pl.E("Failed to store video %q after Metarr cancellation: %v", video.URL, err)
			}
		}

		// Acquire channel and global Metarr slots.
		release, ok := acquireMetarrSlots(vMetarrCtx, b.stopRequested, channel, channelURL)
		if !ok {
			storePending()
			return
		}
		defer release()
		if vMetarrCtx.Err() != nil || isClosed(b.stopRequested) {
			storePending()
			return
		}
		claim.startedAt.Store(time.Now().UnixMilli())

		video.MetarrStatus = execMetarr(vMetarrCtx, video, channelURL, channel, b.dirParser)

		if err := completeAndStoreVideo(b.cs, b.vs, video, channel); err != nil {
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

	// Finish tracking video which is queued or downloading = set 'Failed'
	if status == consts.DLStatusQueued || status == consts.DLStatusDownloading {
		status = consts.DLStatusFailed
		if errors.Is(context.Cause(vidCtx), downloads.ErrCanceledByUser) {
			status = consts.DLStatusCanceled
		}
	}

	// Finish tracking video which is NOT queued or downloading = inherit status from model
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
) (proceed bool, err error) {
	if v == nil || cu == nil || c == nil {
		return false, fmt.Errorf("invalid nil parameter (video: %v, channelURL: %v, channel: %v)", v == nil, cu == nil, c == nil)
	}

	// ONLY download JSON if it's NOT a custom file (custom scraper already created it).
	if v.JSONCustomFile == "" {
		dl, err := downloads.NewJSONDownload(procCtx, v, cu, c, dlTracker.GetStore(), dlTracker, &downloads.Options{
			MaxRetries:       3,
			RetryMaxInterval: 5,
		})
		if err != nil {
			return false, err
		}

		if err = dl.Execute(); err != nil {
			return false, err
		}
	} else {
		logger.Pl.D(1, "Skipping JSON download - using custom scraped file: %s", v.JSONCustomFile)
	}

	// Validate and filter.
	passedChecks, useFilteredMetaOps, useFilteredFilenameOps, err := metadata.ValidateAndFilter(v, cu, c, dirParser)
	if err != nil {
		return false, err
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
			return false, fmt.Errorf("failed to update ignored video: %w", err)
		}
		return false, nil
	}

	// Will download this video (passed checks).
	v.DownloadStatus.Status = consts.DLStatusQueued
	v.DownloadStatus.Percent = 0.0
	logger.Pl.D(1, "Setting video %q to Queued status before saving to DB", v.URL)

	// Save video to database.
	if v.ID, err = vs.AddVideo(v, c.ID, cu.ID); err != nil {
		return false, fmt.Errorf("failed to update video DB entry: %w", err)
	}

	logger.Pl.S("Processed metadata for: %s", v.URL)
	return true, nil
}

// processVideo processes video downloads.
func processVideo(vidCtx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, dlTracker *downloads.DownloadTracker) error {
	if v == nil || cu == nil || c == nil {
		return fmt.Errorf("invalid nil parameter (video: %v, channelURL: %v, channel: %v)", v == nil, cu == nil, c == nil)
	}

	logger.Pl.I("Processing video download for URL: %s", v.URL)

	dl, err := downloads.NewVideoDownload(vidCtx, v, cu, c, dlTracker.GetStore(), dlTracker, &downloads.Options{
		MaxRetries:       3,
		RetryMaxInterval: 20,
	})
	if err != nil {
		return err
	}

	if err = dl.Execute(); err != nil {
		return err
	}

	logger.Pl.D(1, "Successfully processed and marked as downloaded: %s", v.URL)
	return nil
}
