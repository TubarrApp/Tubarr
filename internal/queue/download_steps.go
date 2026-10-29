package queue

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"os"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/downloads"
	"tubarr/internal/file"
	"tubarr/internal/limits"
	"tubarr/internal/metadata"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/scraper"
	"tubarr/internal/times"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// run takes a video through its download, then hands it to post-processing.
func (q *DownloadQueue) run(d *download) {
	// Holding the video cancels any Metarr run on it, so the run and this download never use its files simultaneously.
	hold, held, err := q.postProcessing.hold(d.startCtx, d.c, d.video.URL)
	if err == nil && (hold == nil || !held) {
		err = fmt.Errorf("video %q is already being downloaded elsewhere in channel %q", d.video.URL, d.c.Name)
	}

	// Check that neither of the above errors triggered.
	if err != nil {
		q.finish(d, err)
		return
	}

	// Download the video, then post-process it if it downloaded.
	err = q.process(d)
	q.finish(d, err)
	if d.video.DownloadStatus.Status != consts.DLStatusCompleted {
		if hold != nil {
			hold.release()
		}
		return
	}
	q.postProcess(d, hold)
}

// process waits for the video's turn, fetches and checks its metadata, then downloads it.
func (q *DownloadQueue) process(d *download) error {
	v, c, cu := d.video, d.c, d.cu
	cs := q.store.ChannelStore()

	// Wait for a slot in the channel's lane.
	release, err := q.acquireLane(d)
	if err != nil {
		return err
	}
	defer release()

	// Fetch the channel URL's cookies, once for every video of the request.
	if err := d.session.ready(q.ctx, cs, c); err != nil {
		return err
	}

	// Fail fast once an earlier video's bot detection has blocked the domain.
	if err := downloads.CheckIfAvoidURL(v, cu, cs.GetDB()); err != nil {
		return err
	}
	if err := DomainBlocked(cu); err != nil {
		return err
	}

	// Bot detection avoidance wait time.
	if err := times.WaitTime(d.startCtx, times.RandomSecsDuration(consts.DefaultBotAvoidanceSeconds), c.Name, "", v.URL); err != nil {
		return err
	}

	// Work out where it goes, and with what options.
	if err := q.prepare(d); err != nil {
		return err
	}
	cu = d.cu // Download As may have given the video yt-dlp options of its own.

	// Process JSON metadata phase.
	d.SetStatus(v, consts.DLStatusQueued, 0, "Fetching metadata", nil)
	proceed, err := processJSON(d.startCtx, q.store.VideoStore(), q.store.DownloadStore(), d, d.dirParser, c, cu, v)
	if err != nil {
		return fmt.Errorf("metadata processing error for video URL %q: %w", v.URL, err)
	}
	if !proceed {
		logger.Pl.I("Skipping further processing for ignored video: %s", v.URL)
		return nil
	}

	// Check move operations.
	v.MoveOpOutputDir = metadata.HandleMoveOps(v, cu, d.dirParser)

	// Wait for a download slot for its domain, then a global one.
	d.SetStatus(v, consts.DLStatusQueued, 0, "Waiting for download slot", nil)
	domainSem, err := limits.DomainDownloads.Acquire(d.startCtx, v.URL)
	if err != nil {
		return err
	}
	defer limits.Release(domainSem)
	globalSem, err := downloads.AcquireGlobalDownloadSlot(d.startCtx, v.URL)
	if err != nil {
		return err
	}
	defer limits.Release(globalSem)

	// Process video download phase, which Stop no longer reaches.
	if !q.startDownloading(d) {
		return context.Cause(d.startCtx)
	}
	return processVideo(d.ctx, v, cu, c, q.store.DownloadStore(), d)
}

// prepare works out where the video goes, and which Download As options and Metarr settings it uses.
func (q *DownloadQueue) prepare(d *download) error {
	v, c, cu := d.video, d.c, d.cu
	d.dirParser = parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)

	// Videos the user chose skip download filters, and use their Download As options.
	if d.byUser {
		v.SkipFilters = true
		var err error
		if v.Overrides, err = q.store.VideoStore().GetVideoOverrides(v.ID); err != nil {
			return err
		}
		if v.Overrides != nil {
			d.cu = withYtdlpOverrides(d.cu, v.Overrides)
		}
	}

	// Don't download anywhere unexpected (e.g. onto the local disk under an unmounted drive's mount point).
	if err := CheckDestinations(c, cu, v.Overrides); err != nil {
		return err
	}

	// Get video paths, filled in for this video rather than kept on the channel URL.
	videoDir, err := d.dirParser.ParseDirectory(cu.ChanURLSettings.VideoDir, "channel URL video directory")
	if err != nil {
		return err
	}
	jsonDir, err := d.dirParser.ParseDirectory(cu.ChanURLSettings.JSONDir, "channel URL JSON directory")
	if err != nil {
		return err
	}
	if o := v.Overrides; o != nil {
		if videoDir, err = overrideDir(d.dirParser, o.VideoDir, videoDir, "Download As video directory"); err != nil {
			return err
		}
		if jsonDir, err = overrideDir(d.dirParser, o.JSONDir, jsonDir, "Download As JSON directory"); err != nil {
			return err
		}
	}
	v.VideoDir = videoDir
	v.JSONDir = jsonDir

	// Download As videos don't use the ops files, so only Metarr being unusable holds them back.
	if v.Overrides == nil || d.session.skipMetarr.Reason == models.SkipMetarrNoPath {
		v.SkipMetarr = d.session.skipMetarr
	}

	// Use custom scraper if needed.
	if v.JSONFilePath, err = executeCustomScraper(d.startCtx, d.session.scraper, v, d.cu.Cookies); err != nil {
		return fmt.Errorf("custom scraper failed: %w", err)
	}
	return nil
}

// acquireLane waits for a slot in the lane of the video's channel, or of its channel URL if that has a limit of its own.
func (q *DownloadQueue) acquireLane(d *download) (release func(), err error) {
	chanLimit := 1
	if d.c.ChanSettings != nil {
		chanLimit = sharedvalidation.ValidateConcurrencyLimit(d.c.ChanSettings.DownloadConcurrency)
	}
	urlLimit := chanLimit
	if s := d.cu.ChanURLSettings; s != nil && s.DownloadConcurrency > 0 {
		urlLimit = sharedvalidation.ValidateConcurrencyLimit(s.DownloadConcurrency)
	}

	lane := q.lanes.lane(d.c, d.cu, chanLimit, urlLimit)
	if lane == nil {
		return nil, nil
	}

	if err := lane.Acquire(d.startCtx); err != nil {
		return nil, err
	}
	return lane.Release, nil
}

// postProcess hands a downloaded video to the post-processing queue, then stores it as finished.
func (q *DownloadQueue) postProcess(d *download, hold *videoHold) {
	if hold == nil {
		logger.Pl.E("Video hold model nil")
		return
	}
	defer hold.release()

	if d == nil {
		logger.Pl.E("Download model nil")
		return
	}
	v := d.video
	vs, cs := q.store.VideoStore(), q.store.ChannelStore()

	if v.SkipMetarr.Skip {
		// Metarr held back: finished with Metarr pending, so Run Metarr can process it once fixed.
		logger.Pl.W("Skipping Metarr for %q, marking it finished with Metarr pending: %s", v.URL, v.SkipMetarr.Reason)
		v.MetarrStatus = consts.MetarrStatusPending
	} else {
		// Store the downloaded file's path first, so startup can recover the video if Tubarr stops during Metarr.
		if err := vs.UpdateVideo(v, d.c.ID); err != nil {
			logger.Pl.E("Failed to store downloaded file path for video %q before Metarr: %v", v.URL, err)
		}
		v.MetarrStatus = hold.postProcess(q.ctx, v, d.cu, d.c, d.dirParser)
	}

	if err := completeAndStoreVideo(cs, vs, v, d.c); err != nil {
		logger.Pl.E("Failed to complete video %q after metarr: %v", v.URL, err)
	}
}

// withYtdlpOverrides returns a copy of cu with the video's Download As yt-dlp options in place of its own, blank ones
// keeping the channel URL's. It's a copy, so the channel URL's other videos keep its options.
func withYtdlpOverrides(cu *models.ChannelURL, o *models.VideoOverrides) *models.ChannelURL {
	s := *cu.ChanURLSettings
	s.YtdlpOutputExt = cmp.Or(o.YtdlpOutputExt, s.YtdlpOutputExt)
	s.MaxFilesize = cmp.Or(o.MaxFilesize, s.MaxFilesize)
	s.ExtraYTDLPVideoArgs = cmp.Or(o.ExtraYTDLPVideoArgs, s.ExtraYTDLPVideoArgs)
	if len(o.YTDLPPreferredVideoCodecs) > 0 {
		s.YTDLPPreferredVideoCodecs = o.YTDLPPreferredVideoCodecs
	}
	if len(o.YTDLPPreferredAudioCodecs) > 0 {
		s.YTDLPPreferredAudioCodecs = o.YTDLPPreferredAudioCodecs
	}

	copied := *cu
	copied.ChanURLSettings = &s
	return &copied
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
func executeCustomScraper(ctx context.Context, s *scraper.Scraper, v *models.Video, cookies []*http.Cookie) (customJSON string, err error) {
	if v == nil || s == nil {
		return "", fmt.Errorf("invalid nil parameter (video: %v, scraper: %v)", v == nil, s == nil)
	}

	// Detect custom site.
	if err := s.ScrapeCustomSite(ctx, v.URL, v.JSONDir, v, cookies); err != nil {
		return "", err
	}

	return v.JSONCustomFile, nil
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
	ctx context.Context,
	vs contracts.VideoStore,
	ds contracts.DownloadStore,
	tracker downloads.Tracker,
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
		dl, err := downloads.NewJSONDownload(ctx, v, cu, c, ds, tracker, &downloads.Options{
			MaxAttempts:      downloadAttempts(cu),
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
func processVideo(ctx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, ds contracts.DownloadStore, tracker downloads.Tracker) error {
	if v == nil || cu == nil || c == nil {
		return fmt.Errorf("invalid nil parameter (video: %v, channelURL: %v, channel: %v)", v == nil, cu == nil, c == nil)
	}

	logger.Pl.I("Processing video download for URL: %s", v.URL)

	dl, err := downloads.NewVideoDownload(ctx, v, cu, c, ds, tracker, &downloads.Options{
		MaxAttempts:      downloadAttempts(cu),
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

// downloadAttempts returns how many times a download runs: once, plus its Download Retries setting (else the default).
func downloadAttempts(cu *models.ChannelURL) int {
	retries := consts.DefaultDownloadRetries
	if cu.ChanURLSettings != nil && cu.ChanURLSettings.Retries > 0 {
		retries = cu.ChanURLSettings.Retries
	}
	return 1 + retries
}
