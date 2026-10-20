package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"tubarr/internal/blocking"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/file"
	"tubarr/internal/limits"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/scraper"
	"tubarr/internal/state"
	"tubarr/internal/times"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"
)

// crawlSems holds the scheduled crawl semaphores, keyed by limit so a changed setting takes effect for new crawls.
var crawlSems sync.Map

// AcquireCrawlSlot waits for a scheduled crawl slot under the crawl concurrency setting (0 = unlimited), or ctx to end.
//
// Returns a function releasing the slot.
func AcquireCrawlSlot(ctx context.Context, channelName string) (release func(), err error) {
	n := abstractions.GetInt(keys.CrawlConcurrency)
	if n <= 0 {
		return func() {}, nil
	}

	// One semaphore per limit, shared by every crawl under that limit.
	v, _ := crawlSems.LoadOrStore(n, make(chan struct{}, n))
	sem := v.(chan struct{})
	if err := limits.AcquireSlot(ctx, sem, "crawl", channelName); err != nil {
		return nil, err
	}
	return func() { <-sem }, nil
}

// CheckChannels checks channels and whether they are due for a crawl.
func CheckChannels(ctx context.Context, s contracts.Store) error {
	// Grab all channels from database.
	cs := s.ChannelStore()
	channels, hasRows, err := cs.GetAllChannels(true)
	if err != nil {
		return err
	}
	if !hasRows {
		logger.Pl.I("No channels in database")
	}

	// Clean expired blocks first.
	if err := blocking.CleanExpiredBlocks(cs.GetDB()); err != nil {
		logger.Pl.W("Failed to clean expired blocks: %v", err)
	}

	// Get crawl concurrency (0 = unlimited, meaning all channels can crawl simultaneously).
	conc := abstractions.GetInt(keys.CrawlConcurrency)
	if conc <= 0 {
		conc = len(channels) // Unlimited: allow all channels to crawl at once.
	}

	var (
		errChan = make(chan error, len(channels))
		sem     = make(chan struct{}, conc)
		wg      sync.WaitGroup
	)

	// Iterate over channels.
	for _, c := range channels {
		if c.LoadErr != nil {
			logger.Pl.W("Skipping crawl: %v", c.LoadErr)
			continue
		}

		// Load in config file.
		file.UpdateFromConfigFile(s.ChannelStore(), c)

		// Ignore channel if paused.
		if c.ChanSettings.Paused {
			logger.Pl.I("Channel with name %q is paused, skipping checks.", c.Name)
			continue
		}

		// Crawl the URLs due, each on its own crawl frequency or the channel's.
		due, next := c.DueURLs(time.Now(), 0, false)
		if len(due) == 0 {
			if next >= 0 {
				logger.Pl.P("Channel %q: next check in %s\n", c.Name, next.Round(time.Second))
			}
			continue
		}
		logger.Pl.I("Channel %q: %d URL(s) due a crawl", c.Name, len(due))
		ch := *c
		ch.URLModels = due

		// Run concurrent jobs.
		wg.Add(1)
		go func(c *models.Channel) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() {
				<-sem
			}()

			// Initiate crawl.
			if performedCrawls, err := CrawlChannel(ctx, s, c); err != nil {
				errChan <- err
			} else if performedCrawls {
				logger.Pl.S("Completed crawl for channel %q", c.Name)
			}
		}(&ch)
	}

	wg.Wait()
	close(errChan)

	allErrs := make([]error, 0, len(channels))
	for e := range errChan {
		allErrs = append(allErrs, e)
	}
	return errors.Join(allErrs...)
}

// ManualDownloadToChannel downloads custom video URLs sent in to the channel.
func ManualDownloadToChannel(ctx context.Context, s contracts.Store, cs contracts.ChannelStore, c *models.Channel, videos []models.ManualVideo) (err error) {
	if c == nil || c.ChanSettings == nil {
		return fmt.Errorf("invalid nil parameters (channel: %v, channel settings: %v)", c == nil, c.ChanSettings == nil)
	}
	if c.LoadErr != nil {
		return c.LoadErr
	}
	if bad := CheckManualVideos(c, videos); len(bad) > 0 {
		return fmt.Errorf("can't download:\n%s", strings.Join(bad, "\n"))
	}
	queueCtx := state.QueueCtx(ctx)

	// Add random sleep before processing (added bot detection).
	if err := times.WaitTime(queueCtx, times.RandomSecsDuration(consts.DefaultBotAvoidanceSeconds), c.Name, "", ""); err != nil {
		return err
	}

	// Load config file settings.
	file.UpdateFromConfigFile(s.ChannelStore(), c)

	// Spawn scraper.
	scrape := scraper.New()

	// Video request model slice.
	customVideoRequests := make([]*models.Video, 0, len(videos))

	// Videos are batched by channel URL and login, each batch with the ChannelURL model it downloads through.
	type manualBatch struct {
		channelURLID int64
		auth         models.ManualAuth
	}
	type batchVideos struct {
		cu     *models.ChannelURL
		videos []*models.Video
	}
	batches := make(map[manualBatch]*batchVideos, len(videos))

	// Video URLs already queued, to skip duplicates.
	queuedURLs := make(map[string]struct{}, len(videos))

	for _, mv := range videos {
		actualVideoURL := mv.URL
		targetChannelURLModel, err := ManualChannelURL(cs, c, mv.ChannelURLID)
		if err != nil {
			return err
		}
		batch := manualBatch{channelURLID: targetChannelURLModel.ID}
		if mv.Auth != nil {
			batch.auth = *mv.Auth
		}
		if b, exists := batches[batch]; exists {
			targetChannelURLModel = b.cu
		} else if mv.Auth != nil {
			withAuth := *targetChannelURLModel // A copy, so the login isn't kept on the channel's model.
			withAuth.Username, withAuth.Password, withAuth.LoginURL = mv.Auth.Username, mv.Auth.Password, mv.Auth.LoginURL
			targetChannelURLModel = &withAuth
		}

		if _, dup := queuedURLs[actualVideoURL]; dup {
			logger.Pl.W("Video %q was entered more than once for channel %q, skipping duplicate", actualVideoURL, c.Name)
			continue
		}
		queuedURLs[actualVideoURL] = struct{}{}

		// Check if this URL's domain is blocked globally for its authentication context.
		parsed, err := url.Parse(targetChannelURLModel.URL)
		if err != nil {
			logger.Pl.W("Unable to parse %q, skipping...", targetChannelURLModel.URL)
			continue
		}
		hostname := parsed.Hostname()
		blockCtx := blocking.GetBlockContext(targetChannelURLModel)
		isBlocked, blockedTime, remaining := blocking.IsBlocked(hostname, blockCtx)
		if isBlocked {
			logger.Pl.W("Skipping video %q for channel %q. Domain is blocked for context %q (Blocked at: %v, Time remaining: %v)",
				actualVideoURL, c.Name, blockCtx, blockedTime.Format("2006-01-02 15:04:05"), remaining.Round(time.Second))
			recordSkip(s.VideoStore(), c, actualVideoURL, fmt.Errorf("%s is blocked for %s requests until %s", hostname, blockCtx, time.Now().Add(remaining).Format("2006-01-02 15:04 MST")))
			continue
		}

		v := &models.Video{
			ChannelID:    c.ID,
			ChannelURLID: targetChannelURLModel.ID,
			ChannelURL:   targetChannelURLModel.URL,
			URL:          actualVideoURL,
			SkipFilters:  true,
		}
		// Known videos (e.g. chosen from Available) keep their ID, so they show on the Downloads page from the start.
		if id, exists := s.VideoStore().VideoExists(v, c.ID); exists {
			v.ID = id
			var overridesErr error
			if v.Overrides, overridesErr = s.VideoStore().GetVideoOverrides(id); overridesErr != nil {
				logger.Pl.E("Skipping video %q for channel %q: %v", actualVideoURL, c.Name, overridesErr)
				recordSkip(s.VideoStore(), c, actualVideoURL, overridesErr)
				continue
			}
		}
		if err := checkDestinations(c, targetChannelURLModel, v.Overrides); err != nil {
			logger.Pl.E("Skipping video %q for channel %q: %v", actualVideoURL, c.Name, err)
			// A video not yet in the channel is kept as failed, so the URL the user entered isn't lost.
			if v.ID == 0 {

				failed := &models.Video{ChannelID: c.ID,
					ChannelURLID: targetChannelURLModel.ID,
					ChannelURL:   targetChannelURLModel.URL,
					URL:          actualVideoURL,
					DownloadStatus: models.DLStatus{
						Status: consts.DLStatusFailed,
					}}

				if id, addErr := s.VideoStore().AddVideo(failed, c.ID, targetChannelURLModel.ID); addErr != nil {
					logger.Pl.E("Failed to store skipped video %q: %v", actualVideoURL, addErr)
				} else if setErr := s.VideoStore().SetMissingVideoErrors([]int64{id}, err.Error()); setErr != nil {
					logger.Pl.E("Failed to store why video %q was skipped: %v", actualVideoURL, setErr)
				}
			}
			recordSkip(s.VideoStore(), c, actualVideoURL, err)
			continue
		}

		b := batches[batch]
		if b == nil {
			b = &batchVideos{cu: targetChannelURLModel}
			batches[batch] = b
		}
		b.videos = append(b.videos, v)
		customVideoRequests = append(customVideoRequests, v)
	}

	if len(customVideoRequests) == 0 {
		return fmt.Errorf("all %d video(s) were skipped, see the errors above", len(videos))
	}

	// Main process.
	var (
		nSucceeded        int
		procError         error
		channelURLsGotNew []string
	)

	// Process each batch's videos.
	for _, b := range batches {
		if queueCtx.Err() != nil {
			procError = errors.Join(procError, queueCtx.Err())
			break
		}
		cu := b.cu

		// Get access details only for channel URLs with videos to download (e.g. avoids needless FlareSolverr solves).
		if cu.Cookies, cu.CookiePath, err = scrape.ScraperURLCookies(queueCtx, cs, c, cu); err != nil {
			procError = errors.Join(procError, err)
			for _, v := range b.videos {
				recordSkip(s.VideoStore(), c, v.URL, err)
			}
			continue
		}

		succeeded, nDownloaded, procErr := InitProcess(ctx, s, c, cu, b.videos, scrape)
		if succeeded != 0 {
			nSucceeded += succeeded
			if nDownloaded > 0 {
				channelURLsGotNew = append(channelURLsGotNew, cu.URL)
			}
		}
		procError = errors.Join(procError, procErr)
	}

	// Update last scan time.
	if err := cs.UpdateLastScan(c.ID); err != nil {
		return fmt.Errorf("failed to update last scan time: %w", err)
	}

	// Handle results and notify services.
	if nSucceeded == 0 {
		return fmt.Errorf("failed to process %d video downloads. Got errors: %w", len(customVideoRequests), procError)
	}

	if err := NotifyServices(cs, c, channelURLsGotNew); err != nil {
		return errors.Join(procError, err)
	}

	if procError != nil {
		return fmt.Errorf("encountered errors during processing: %w", procError)
	}
	return nil
}

// UnavailableDestinations returns why videos can't be downloaded now, as their channel URL's destination is unavailable
// (e.g. an unmounted drive).
func UnavailableDestinations(c *models.Channel, videos []models.ManualVideo) (bad []string) {
	for _, mv := range videos {
		cu := &models.ChannelURL{} // Manual downloads use the channel's settings.
		if found := c.URLByID(mv.ChannelURLID); mv.ChannelURLID != 0 && found != nil {
			cu = found
		}
		if err := checkDestinations(c, cu, nil); err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", mv.URL, err))
		}
	}
	return bad
}

// recordSkip stores why a claimed video wasn't downloaded, shown once it's back in the Available list.
func recordSkip(vs contracts.VideoStore, c *models.Channel, videoURL string, reason error) {
	if err := vs.SetQueuedVideoError(c.ID, videoURL, reason.Error()); err != nil {
		logger.Pl.E("Failed to store why video %q was skipped: %v", videoURL, err)
	}
}

// AvailableStatuses are the statuses of videos the user can choose to download or dismiss.
var AvailableStatuses = []consts.DownloadStatus{consts.DLStatusAvailable, consts.DLStatusFailed, consts.DLStatusCanceled}

// RemoveDismissedFromNew drops dismissed videos from the channel's new video list, clearing its notification dot if none remain.
func RemoveDismissedFromNew(cs contracts.ChannelStore, c *models.Channel, dismissed []*models.Video) {
	urls := make([]string, 0, len(dismissed))
	for _, v := range dismissed {
		urls = append(urls, v.URL)
	}
	if err := cs.RemoveNewVideoURLs(c.ID, urls); err != nil {
		logger.Pl.E("Failed to update new video URLs for channel %q: %v", c.Name, err)
	}
}

// DownloadAvailable downloads videos claimed from the Available list, once any running crawl or download of the channel finishes.
//
// Claimed videos the download didn't reach are marked canceled if the channel was stopped, or failed otherwise (e.g. their
// domain is blocked), so they can be chosen again. Login details given (or nil) are used for this download only.
func DownloadAvailable(parent context.Context, s contracts.Store, c *models.Channel, claimed []*models.Video, auth *models.ManualAuth) (err error) {
	ids := make([]int64, 0, len(claimed))
	for _, v := range claimed {
		ids = append(ids, v.ID)
	}
	ctx, unlock, err := state.LockCrawl(parent, c.ID)
	if err == nil {
		defer unlock()
		err = downloadClaimed(ctx, s, c, claimed, auth)
	}

	unreached := consts.DLStatusFailed
	if errors.Is(err, state.ErrStoppedWaiting) || (ctx != nil && state.QueueCtx(ctx).Err() != nil) {
		unreached = consts.DLStatusCanceled
	}
	reset, resetErr := s.VideoStore().ClaimVideos(c.ID, ids, []consts.DownloadStatus{consts.DLStatusQueued}, unreached)
	if resetErr != nil {
		err = errors.Join(err, resetErr)
	}

	// Failed videos without a reason of their own get the batch's.
	if unreached == consts.DLStatusFailed && len(reset) > 0 {
		reason := "skipped before downloading, see the Tubarr log for why"
		if err != nil {
			reason = err.Error()
		}
		resetIDs := make([]int64, 0, len(reset))
		for _, v := range reset {
			resetIDs = append(resetIDs, v.ID)
		}
		if reasonErr := s.VideoStore().SetMissingVideoErrors(resetIDs, reason); reasonErr != nil {
			err = errors.Join(err, reasonErr)
		}
	}
	return err
}

// downloadClaimed downloads claimed videos through their channel URLs.
func downloadClaimed(ctx context.Context, s contracts.Store, c *models.Channel, claimed []*models.Video, auth *models.ManualAuth) error {
	videos := make([]models.ManualVideo, 0, len(claimed))
	for _, v := range claimed {
		mv := models.ManualVideo{URL: v.URL, Auth: auth} // No channel URL if since removed, to download as a manual video.
		if c.URLByID(v.ChannelURLID) != nil {
			mv.ChannelURLID = v.ChannelURLID
		}
		videos = append(videos, mv)
	}
	return ManualDownloadToChannel(ctx, s, s.ChannelStore(), c, videos)
}

// CrawlChannel crawls a channel for new URLs.
func CrawlChannel(ctx context.Context, s contracts.Store, c *models.Channel) (performedCrawls bool, err error) {
	if c == nil {
		return false, fmt.Errorf("channel cannot be nil")
	}
	if c.LoadErr != nil {
		return false, c.LoadErr
	}
	if len(c.URLModels) == 0 {
		return false, nil
	}

	// The URLs given count as crawled however the crawl ends, so each waits its crawl frequency again.
	urlIDs := make([]int64, 0, len(c.URLModels))
	for _, cu := range c.URLModels {
		if !cu.IsManual {
			urlIDs = append(urlIDs, cu.ID)
		}
	}
	markScanned := func() error {
		if err := s.ChannelStore().UpdateChannelURLsLastScan(urlIDs); err != nil {
			return err
		}
		return s.ChannelStore().UpdateLastScan(c.ID)
	}

	// Filter out any URLs that are blocked globally for their authentication context.
	var hasAllowed bool
	if c.URLModels, hasAllowed = filterAllowedValidURLs(c); !hasAllowed {
		logger.Pl.D(4, "No URLs for channel %q can be crawled (blocked, or destination unavailable), skipping crawl", c.Name)
		// Counts as a scan, so the URLs wait for their next crawl instead of being retried every check.
		if err := markScanned(); err != nil {
			return false, fmt.Errorf("failed to update last scan time: %w", err)
		}
		return false, nil
	}

	// Check URLs to crawl.
	urlsToCrawl := 0
	for _, cu := range c.URLModels {
		if cu.Paused() {
			logger.Pl.D(1, "Skipping paused channel URL %q...", cu.URL)
			continue
		}
		if !cu.IsManual {
			urlsToCrawl++
		}
	}
	if urlsToCrawl == 0 {
		logger.Pl.I("No monitored URLs for channel %q", c.Name)
		return false, nil
	}

	// Get channel store.
	cs := s.ChannelStore()
	queueCtx := state.QueueCtx(ctx)

	// Add random sleep before processing (added bot detection).
	if err := times.WaitTime(queueCtx, times.RandomSecsDuration(consts.DefaultBotAvoidanceSeconds), c.Name, "", ""); err != nil {
		return false, err
	}

	// Check validity of default output directories.
	if c.ChanSettings.VideoDir == "" || c.ChanSettings.JSONDir == "" {
		return false, errors.New("default channel output directories are blank")
	}

	fmt.Println()
	logger.Pl.I("%sINITIALIZING CRAWL:%s Channel %q:\n", sharedconsts.ColorGreen, sharedconsts.ColorReset, c.Name)

	// Get new releases for channel.
	scrape := scraper.New()
	videos, crawlErr := scrape.GetNewReleases(queueCtx, cs, c, false)
	if crawlErr != nil && len(videos) == 0 {
		if updateErr := markScanned(); updateErr != nil {
			logger.Pl.E("Failed to update channel %q last scan time after failure to crawl channel: %v", c.Name, updateErr)
		}
		return false, crawlErr
	}

	if len(videos) == 0 {
		logger.Pl.I("No new releases found for channel %q", c.Name)
		if err := markScanned(); err != nil {
			return true, fmt.Errorf("no new releases found for channel %q. Failed to update last scan time: %w", c.Name, err)
		}
		return true, nil
	}

	// Main process (a partial crawl's errors are reported alongside processing errors).
	var (
		nSucceeded, nDownloaded, nAvailable int
		procError                           = crawlErr
		channelURLsGotNew                   []string
	)

	// Process channel URLs.
	for _, cu := range c.URLModels {
		// Skip manual or paused channel entry.
		if cu.IsManual || cu.Paused() {
			continue
		}

		// Get requests for this channel.
		var vRequests []*models.Video
		for _, v := range videos {
			if v.ChannelURLID == cu.ID {
				vRequests = append(vRequests, v)
			}
		}
		if len(vRequests) == 0 {
			continue
		}
		if queueCtx.Err() != nil {
			procError = errors.Join(procError, queueCtx.Err())
			break
		}

		logger.Pl.I("Got %d video(s) for URL %q %s(Channel: %s)%s", len(vRequests), cu.URL, sharedconsts.ColorGreen, c.Name, sharedconsts.ColorReset)

		// Leave for the user to choose from if not auto downloading.
		if !c.AutoDownloads(cu) {
			addedURLs, err := s.VideoStore().AddAvailableVideos(vRequests, c.ID)
			if err != nil {
				procError = errors.Join(procError, err)
				continue
			}
			if err := cs.AddNewVideoURLs(c.ID, addedURLs); err != nil {
				logger.Pl.E("Failed to set notification dot for channel %q: %v", c.Name, err)
			}
			logger.Pl.S("Added %d video(s) as available for URL %q %s(Channel: %s)%s", len(addedURLs), cu.URL, sharedconsts.ColorGreen, c.Name, sharedconsts.ColorReset)
			nAvailable += len(addedURLs)
			continue
		}

		// Process video batch.
		succeeded, downloaded, procErr := InitProcess(ctx, s, c, cu, vRequests, scrape)

		// Succeeded/downloaded.
		if succeeded != 0 {
			nSucceeded += succeeded
			if downloaded > 0 {
				logger.Pl.S("Successfully downloaded %d video(s) for URL %q %s(Channel: %s)%s", downloaded, cu.URL, sharedconsts.ColorGreen, c.Name, sharedconsts.ColorReset)
				channelURLsGotNew = append(channelURLsGotNew, cu.URL)
				nDownloaded += downloaded
			}
		}
		procError = errors.Join(procError, procErr)
	}

	// Last scan time update.
	if err := markScanned(); err != nil {
		return true, fmt.Errorf("failed to update last scan time: %w", err)
	}

	// All videos failed.
	if nSucceeded == 0 && nAvailable == 0 && procError != nil {
		return true, fmt.Errorf("failed to process %d video downloads. Got errors: %w", len(videos), procError)
	}

	// Some succeeded, notify URLs.
	if nDownloaded > 0 {
		if err := NotifyServices(cs, c, channelURLsGotNew); err != nil {
			return true, errors.Join(procError, err)
		}
	}

	// Some errors encountered.
	if procError != nil {
		return true, fmt.Errorf("channel %q encountered errors processing: %w", c.Name, procError)
	}

	return true, nil
}

// CrawlChannelIgnore gets the channel's currently displayed videos and marks them as complete without downloading.
func CrawlChannelIgnore(ctx context.Context, s contracts.Store, c *models.Channel) error {
	if c == nil {
		return fmt.Errorf("channel cannot be nil")
	}
	if c.LoadErr != nil {
		return c.LoadErr
	}
	cs := s.ChannelStore()

	// Global domain blocking is checked per-URL during scraping.

	// Load in config file.
	file.UpdateFromConfigFile(cs, c)

	// Get new releases.
	scrape := scraper.New()
	videos, crawlErr := scrape.GetNewReleases(state.QueueCtx(ctx), cs, c, true)
	if crawlErr != nil && len(videos) == 0 {
		return crawlErr
	}

	// Add videos to ignore.
	if len(videos) > 0 {
		toIgnore := make([]*models.Video, 0, len(videos))
		for _, v := range videos {
			if v.URL == "" {
				logger.Pl.D(5, "Skipping invalid video entry with no URL in channel %q", c.Name)
				continue
			}
			v.MarkVideoAsIgnored()
			toIgnore = append(toIgnore, v)
		}

		validVideos, err := s.VideoStore().AddVideos(toIgnore, c.ID)
		if err != nil {
			logger.Pl.E("Encountered the following errors adding videos for channel %q: %v", c.Name, err)

			if len(validVideos) == 0 {
				return fmt.Errorf("no videos were successfully added to the ignore list for channel with ID %d", c.ID)
			}
		}

		logger.Pl.S("Added %d videos to the ignore list in channel %q", len(validVideos), c.Name)
	}
	return crawlErr
}

// filterAllowedValidURLs returns only valid and unblocked URLs.
func filterAllowedValidURLs(c *models.Channel) ([]*models.ChannelURL, bool) {
	// Filter out manual URLs and globally blocked URLs.
	allowedURLModels := make([]*models.ChannelURL, 0, len(c.URLModels))
	for _, cu := range c.URLModels {
		if cu.IsManual {
			continue
		}

		parsed, err := url.Parse(cu.URL)
		if err != nil {
			logger.Pl.W("Unable to parse %q, skipping...", cu.URL)
			continue
		}

		hostname := parsed.Hostname()

		// Check if this URL is blocked globally for its authentication context.
		blockCtx := blocking.GetBlockContext(cu)
		isBlocked, blockedTime, remaining := blocking.IsBlocked(hostname, blockCtx)
		if isBlocked {
			logger.Pl.D(2, "Skipping URL %q for channel %q. Domain %q is blocked for context %q (Blocked at: %v, Time remaining: %v)", cu.URL, c.Name, hostname, blockCtx, blockedTime.Format("2006-01-02 15:04:05"), remaining.Round(time.Second))
			continue
		}

		// Don't download anywhere unexpected (e.g. onto the local disk under an unmounted drive's mount point).
		if err := checkDestinations(c, cu, nil); err != nil {
			logger.Pl.E("Skipping URL %q for channel %q: %v", cu.URL, c.Name, err)
			continue
		}

		// Hostname not blocked, safe to proceed.
		allowedURLModels = append(allowedURLModels, cu)
	}
	return allowedURLModels, len(allowedURLModels) > 0
}

// checkDestinations returns an error if any of a channel URL's download destinations (or a video's overrides) isn't available.
func checkDestinations(c *models.Channel, cu *models.ChannelURL, o *models.VideoOverrides) error {
	s := cmp.Or(cu.ChanURLSettings, c.ChanSettings)
	var dirs []string
	if s != nil {
		videoDir, jsonDir := s.VideoDir, s.JSONDir
		if o != nil {
			videoDir, jsonDir = cmp.Or(o.VideoDir, videoDir), cmp.Or(o.JSONDir, jsonDir)
		}
		dirs = append(dirs, videoDir, jsonDir)
	}
	// Metarr's output directory, in the order metarr.parseMetarrOutputDir picks it.
	switch {
	case o != nil && o.OutputDir != "":
		dirs = append(dirs, o.OutputDir)
	case abstractions.IsSet(keys.MoveOnComplete):
		dirs = append(dirs, abstractions.GetString(keys.MoveOnComplete))
	default:
		if s != nil {
			for _, op := range s.MetaFilterMoveOps {
				dirs = append(dirs, op.OutputDir)
			}
		}
		if m := cmp.Or(cu.ChanURLMetarrArgs, c.ChanMetarrArgs); m != nil {
			dirs = append(dirs, m.OutputDir)
		}
	}
	for _, dir := range dirs {
		if err := validation.CheckDestination(dir); err != nil {
			return err
		}
	}
	return nil
}

// ensureManualDownloadsChannelURL ensures a special "manual-downloads" ChannelURL exists for the channel.
func ensureManualDownloadsChannelURL(cs contracts.ChannelStore, c *models.Channel) (*models.ChannelURL, error) {
	// First check in-memory models.
	for _, cu := range c.URLModels {
		if cu.IsManual {
			return cu, nil // Return existing model.
		}
	}

	// If not found in memory, check database directly.
	existingCU, hasRows, err := cs.GetChannelURLModel(c.ID, consts.ManualDownloadsCol, true)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing manual downloads URL: %w", err)
	}
	if hasRows && existingCU != nil {
		c.URLModels = append(c.URLModels, existingCU)
		return existingCU, nil
	}

	// Get channel to inherit settings.
	manualChanURL := &models.ChannelURL{
		URL: consts.ManualDownloadsCol,
	}

	// Create it if it doesn't exist.
	if _, err = cs.AddChannelURL(c.ID, manualChanURL, true); err != nil {
		return nil, fmt.Errorf("failed to create manual downloads channel URL: %w", err)
	}

	// Fetch the newly created ChannelURL with merged parent settings.
	fetchedCU, hasRows, err := cs.GetChannelURLModel(c.ID, consts.ManualDownloadsCol, true)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch newly created manual downloads URL: %w", err)
	}
	if !hasRows || fetchedCU == nil {
		return nil, fmt.Errorf("newly created manual downloads URL not found in database")
	}

	// Add to in-memory models.
	c.URLModels = append(c.URLModels, fetchedCU)

	return fetchedCU, nil
}

// blockChannelBotDetected blocks a domain globally due to bot detection on the given URL.
func blockChannelBotDetected(cs contracts.ChannelStore, cu *models.ChannelURL, videoURL string) error {
	// Fall back to the channel URL if the video URL can't be parsed.
	var hostname string
	parsedURL, err := url.Parse(videoURL)
	if err == nil {
		hostname = parsedURL.Hostname()
	}
	if hostname == "" {
		// Fall back to channel URL if video URL is invalid.
		logger.Pl.E("Could not parse video URL %q, falling back to channel URL", videoURL)
		parsedURL, err = url.Parse(cu.URL)
		if err != nil {
			return fmt.Errorf("failed to parse channel URL %q for blocking: %w", cu.URL, err)
		}
		hostname = parsedURL.Hostname()
	}

	// Determine the block context based on authentication.
	blockCtx := blocking.GetBlockContext(cu)

	// Block the domain globally for this context.
	db := cs.GetDB()
	if err := blocking.BlockDomain(db, hostname, blockCtx); err != nil {
		return fmt.Errorf("failed to block domain %q (context: %s): %w", hostname, blockCtx, err)
	}

	return nil
}

// CheckManualVideos returns the videos that can't be sent in to the channel, with why. Each must be a full http(s) video
// URL, with no channel URL or one of the channel's.
func CheckManualVideos(c *models.Channel, videos []models.ManualVideo) (bad []string) {
	for _, mv := range videos {
		switch {
		case !validation.IsHTTPURL(mv.URL):
			bad = append(bad, fmt.Sprintf("%q: not a full http(s) URL", mv.URL))
		case mv.ChannelURLID != 0 && c.URLByID(mv.ChannelURLID) == nil:
			bad = append(bad, fmt.Sprintf("%q: channel URL ID %d isn't one of this channel's", mv.URL, mv.ChannelURLID))
		case mv.Auth != nil && !ValidManualAuth(mv.Auth):
			bad = append(bad, fmt.Sprintf("%q: authentication needs a username and a full http(s) login URL", mv.URL))
		}
	}
	return bad
}

// ValidManualAuth reports whether login details sent with manual videos have a username and a full http(s) login URL.
func ValidManualAuth(a *models.ManualAuth) bool {
	return a.Username != "" && validation.IsHTTPURL(a.LoginURL)
}

// ManualChannelURL returns the channel's URL with the given ID, or its manual downloads URL for 0. A video whose channel
// URL was removed has ID 0, so it's treated as a manual download.
func ManualChannelURL(cs contracts.ChannelStore, c *models.Channel, id int64) (*models.ChannelURL, error) {
	if id == 0 {
		return ensureManualDownloadsChannelURL(cs, c)
	}
	if cu := c.URLByID(id); cu != nil {
		return cu, nil
	}
	return nil, fmt.Errorf("channel URL ID %d isn't one of channel %q's", id, c.Name)
}

// AddManualAvailable adds video URLs to the channel's Available list, returning the added videos and the URLs already
// in the channel.
func AddManualAvailable(s contracts.Store, c *models.Channel, videos []models.ManualVideo) (added []*models.Video, existing []string, err error) {
	if bad := CheckManualVideos(c, videos); len(bad) > 0 {
		return nil, nil, fmt.Errorf("can't add:\n%s", strings.Join(bad, "\n"))
	}
	requests := make([]*models.Video, 0, len(videos))
	for _, mv := range videos {
		if slices.ContainsFunc(requests, func(v *models.Video) bool { return v.URL == mv.URL }) {
			continue
		}
		cu, err := ManualChannelURL(s.ChannelStore(), c, mv.ChannelURLID)
		if err != nil {
			return nil, nil, err
		}
		requests = append(requests, &models.Video{ChannelID: c.ID, ChannelURLID: cu.ID, ChannelURL: cu.URL, URL: mv.URL})
	}
	addedURLs, err := s.VideoStore().AddAvailableVideos(requests, c.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, v := range requests {
		if slices.Contains(addedURLs, v.URL) {
			added = append(added, v)
		} else {
			existing = append(existing, v.URL)
		}
	}
	return added, existing, nil
}

// AddChannelFromInput builds the channel input describes and adds it, with its logins and notifications.
//
// Errors in the input wrap contracts.ErrInvalidChannel.
func AddChannelFromInput(cs contracts.ChannelStore, input models.ChannelInput) (*models.Channel, error) {
	c, authMap, err := parsing.BuildChannelFromInput(input)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", contracts.ErrInvalidChannel, err)
	}
	var notifications []*models.Notification
	if input.Notification != nil {
		if notifications, err = parsing.ParseNotifications(*input.Notification); err != nil {
			return nil, fmt.Errorf("%w: invalid notification URLs: %w", contracts.ErrInvalidChannel, err)
		}
	}
	logger.Pl.I("Adding channel %q to Tubarr...", c.Name)
	if err := cs.AddChannelWithDetails(c, authMap, notifications); err != nil {
		return nil, err
	}
	return c, nil
}
