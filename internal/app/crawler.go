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
	"tubarr/internal/scraper"
	"tubarr/internal/state"
	"tubarr/internal/times"
	"tubarr/internal/validation"

	"github.com/TubarrApp/gocommon/abstractions"
	"github.com/TubarrApp/gocommon/sharedconsts"

	"golang.org/x/net/publicsuffix"
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
	v, _ := crawlSems.LoadOrStore(n, make(chan struct{}, n))
	sem, _ := v.(chan struct{})
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
		// Load in config file.
		file.UpdateFromConfigFile(s.ChannelStore(), c)

		// Ignore channel if paused.
		if c.ChanSettings.Paused {
			logger.Pl.I("Channel with name %q is paused, skipping checks.", c.Name)
			continue
		}

		// Time for a scan?
		crawlFreq := c.GetCrawlFreq()
		timeSinceLastScan := time.Since(c.LastScan)

		logger.Pl.I("Time since last check for channel %q: %s\nCrawl frequency: %d minutes",
			c.Name,
			timeSinceLastScan.Round(time.Second),
			crawlFreq)

		if crawlFreq > 0 {
			crawlFreqDuration := time.Duration(crawlFreq) * time.Minute
			if timeSinceLastScan < crawlFreqDuration {
				remainingTime := crawlFreqDuration - timeSinceLastScan
				logger.Pl.P("Next check in: %s", remainingTime.Round(time.Second))
				fmt.Println()
				continue
			}
		}

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
		}(c)
	}

	wg.Wait()
	close(errChan)

	// Aggregate errors.
	allErrs := make([]error, 0, len(channels))
	for e := range errChan {
		allErrs = append(allErrs, e)
	}

	if len(allErrs) > 0 {
		return errors.Join(allErrs...)
	}

	return nil
}

// ManualDownloadToChannel downloads custom video URLs sent in to the channel.
func ManualDownloadToChannel(ctx context.Context, s contracts.Store, cs contracts.ChannelStore, c *models.Channel, videoURLs []string) (err error) {
	if c == nil || c.ChanSettings == nil {
		return fmt.Errorf("invalid nil parameters (channel: %v, channel settings: %v)", c == nil, c.ChanSettings == nil)
	}
	queueCtx := state.QueueCtx(ctx)

	// Add random sleep before processing (added bot detection).
	if err := times.WaitTime(queueCtx, times.RandomSecsDuration(consts.DefaultBotAvoidanceSeconds), c.Name, ""); err != nil {
		return err
	}

	// Load config file settings.
	file.UpdateFromConfigFile(s.ChannelStore(), c)

	// Spawn scraper.
	scrape := scraper.New()

	// Build a map of channel URL -> ChannelURL model for quick lookup.
	channelURLMap := make(map[string]*models.ChannelURL, len(c.URLModels))
	for _, cu := range c.URLModels {
		channelURLMap[cu.URL] = cu
	}

	// Video request model slice.
	customVideoRequests := make([]*models.Video, 0, len(videoURLs))

	// Track which ChannelURL model each video uses (by ChannelURLID).
	channelURLModels := make(map[int64]*models.ChannelURL, len(videoURLs))

	// Video URLs already queued, to skip duplicates.
	queuedURLs := make(map[string]struct{}, len(videoURLs))

	for _, videoURL := range videoURLs {
		var (
			targetChannelURLModel *models.ChannelURL
			actualVideoURL        string
		)

		// Parse pipe-delimited format: "channelURL|videoURL".
		if strings.Contains(videoURL, "|") {
			targetChannelURLModel, actualVideoURL, err = parsePipedVideoURL(videoURL, channelURLMap)
			if err != nil {
				return err
			}
		} else {
			targetChannelURLModel, actualVideoURL, err = parseManualVideoURL(cs, c, videoURL)
			if err != nil {
				return err
			}
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
			continue
		}
		if err := checkDestinations(c, targetChannelURLModel); err != nil {
			logger.Pl.E("Skipping video %q for channel %q: %v", actualVideoURL, c.Name, err)
			continue
		}

		// // Check if already downloaded.
		// if _, exists := existingVideoURLsMap[actualVideoURL]; exists {
		// 	return fmt.Errorf("video %q already downloaded to this channel, please delete it using 'delete-videos' first if you wish to re-download it", actualVideoURL)
		// }

		// Store the model for later use.
		channelURLModels[targetChannelURLModel.ID] = targetChannelURLModel

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
		}
		customVideoRequests = append(customVideoRequests, v)
	}

	// Fill output directories and batch channels together.
	videosByChannelURL := make(map[int64][]*models.Video)
	for _, v := range customVideoRequests {
		videosByChannelURL[v.ChannelURLID] = append(videosByChannelURL[v.ChannelURLID], v)
	}

	// Main process.
	var (
		nSucceeded        int
		procError         error
		channelURLsGotNew []string
	)

	// Process each ChannelURL's videos.
	for channelURLID, videos := range videosByChannelURL {
		if len(videos) == 0 {
			continue
		}
		if queueCtx.Err() != nil {
			procError = errors.Join(procError, queueCtx.Err())
			break
		}

		// Get the ChannelURL model stored earlier.
		cu, exists := channelURLModels[channelURLID]
		if !exists {
			logger.Pl.E("Could not find ChannelURL model for ID %d", channelURLID)
			continue
		}

		// Get access details only for channel URLs with videos to download (e.g. avoids needless FlareSolverr solves).
		if cu.Cookies, cu.CookiePath, err = scrape.ScraperURLCookies(queueCtx, cs, c, cu); err != nil {
			procError = errors.Join(procError, err)
			continue
		}

		succeeded, nDownloaded, procErr := InitProcess(ctx, s, c, cu, videos, scrape)
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

// AvailableStatuses are the statuses of videos the user can choose to download or dismiss.
var AvailableStatuses = []consts.DownloadStatus{consts.DLStatusAvailable, consts.DLStatusFailed, consts.DLStatusCanceled}

// DownloadAvailable downloads videos claimed from the Available list, once any running crawl or download of the channel finishes.
//
// Claimed videos the download didn't reach are marked canceled if the channel was stopped, or failed otherwise (e.g. their
// domain is blocked), so they can be chosen again.
func DownloadAvailable(parent context.Context, s contracts.Store, c *models.Channel, claimed []*models.Video) (err error) {
	ids := make([]int64, 0, len(claimed))
	for _, v := range claimed {
		ids = append(ids, v.ID)
	}
	ctx, unlock, err := state.LockCrawl(parent, c.ID)
	if err == nil {
		defer unlock()
		err = downloadClaimed(ctx, s, c, claimed)
	}

	unreached := consts.DLStatusFailed
	if errors.Is(err, state.ErrStoppedWaiting) || (ctx != nil && state.QueueCtx(ctx).Err() != nil) {
		unreached = consts.DLStatusCanceled
	}
	if _, resetErr := s.VideoStore().ClaimVideos(c.ID, ids, []consts.DownloadStatus{consts.DLStatusQueued}, unreached); resetErr != nil {
		err = errors.Join(err, resetErr)
	}
	return err
}

// downloadClaimed downloads claimed videos through their channel URLs.
func downloadClaimed(ctx context.Context, s contracts.Store, c *models.Channel, claimed []*models.Video) error {
	urls := make([]string, 0, len(claimed))
	for _, v := range claimed {
		if slices.ContainsFunc(c.URLModels, func(cu *models.ChannelURL) bool { return cu.URL == v.ChannelURL }) {
			urls = append(urls, v.ChannelURL+"|"+v.URL)
		} else {
			urls = append(urls, v.URL) // Channel URL since removed, download as a manual video.
		}
	}
	return ManualDownloadToChannel(ctx, s, s.ChannelStore(), c, urls)
}

// CrawlChannel crawls a channel for new URLs.
func CrawlChannel(ctx context.Context, s contracts.Store, c *models.Channel) (performedCrawls bool, err error) {
	if c == nil {
		return false, fmt.Errorf("channel cannot be nil")
	}
	if len(c.URLModels) == 0 {
		return false, nil
	}

	// Filter out any URLs that are blocked globally for their authentication context.
	var hasAllowed bool
	if c.URLModels, hasAllowed = filterAllowedValidURLs(c); !hasAllowed {
		logger.Pl.D(4, "No URLs for channel %q can be crawled (blocked, or destination unavailable), skipping crawl", c.Name)
		// Counts as a scan, so the channel waits for its next crawl instead of being retried every check.
		if err := s.ChannelStore().UpdateLastScan(c.ID); err != nil {
			return false, fmt.Errorf("failed to update last scan time: %w", err)
		}
		return false, nil
	}

	// Check URLs to crawl.
	urlsToCrawl := 0
	for _, cu := range c.URLModels {
		if !cu.IsManual && (cu.ChanURLSettings != nil && !cu.ChanURLSettings.Paused) {
			urlsToCrawl++
		}

		if cu.ChanURLSettings != nil && cu.ChanURLSettings.Paused {
			logger.Pl.D(1, "Skipping paused channel URL %q...", cu.URL)
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
	if err := times.WaitTime(queueCtx, times.RandomSecsDuration(consts.DefaultBotAvoidanceSeconds), c.Name, ""); err != nil {
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
		if updateErr := cs.UpdateLastScan(c.ID); updateErr != nil {
			logger.Pl.E("Failed to update channel %q last scan time after failure to crawl channel: %v", c.Name, updateErr)
		}
		return false, crawlErr
	}

	if len(videos) == 0 {
		logger.Pl.I("No new releases found for channel %q", c.Name)
		if err := cs.UpdateLastScan(c.ID); err != nil {
			return true, fmt.Errorf("no new releases found for channel %q. Failed to update last scan time: %w", c.Name, err)
		}
		return true, nil
	}

	// Main process (a partial crawl's errors are reported alongside processing errors).
	var (
		nSucceeded,
		nDownloaded,
		nAvailable int
		procError         = crawlErr
		channelURLsGotNew []string
	)

	// Process channel URLs.
	for _, cu := range c.URLModels {
		// Skip manual or paused channel entry.
		if cu.IsManual || (cu.ChanURLSettings != nil && cu.ChanURLSettings.Paused) {
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
	if err := cs.UpdateLastScan(c.ID); err != nil {
		return true, fmt.Errorf("failed to update last scan time: %w", err)
	}

	// All videos failed.
	if nSucceeded == 0 && nAvailable == 0 {
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
		if domain, err := publicsuffix.EffectiveTLDPlusOne(hostname); err == nil { // If err IS nil.
			hostname = strings.ToLower(domain)
		}

		// Check if this URL is blocked globally for its authentication context.
		blockCtx := blocking.GetBlockContext(cu)
		isBlocked, blockedTime, remaining := blocking.IsBlocked(hostname, blockCtx)
		if isBlocked {
			logger.Pl.D(2, "Skipping URL %q for channel %q. Domain %q is blocked for context %q (Blocked at: %v, Time remaining: %v)", cu.URL, c.Name, hostname, blockCtx, blockedTime.Format("2006-01-02 15:04:05"), remaining.Round(time.Second))
			continue
		}

		// Don't download anywhere unexpected (e.g. onto the local disk under an unmounted drive's mount point).
		if err := checkDestinations(c, cu); err != nil {
			logger.Pl.E("Skipping URL %q for channel %q: %v", cu.URL, c.Name, err)
			continue
		}

		// Hostname not blocked, safe to proceed.
		allowedURLModels = append(allowedURLModels, cu)
	}
	return allowedURLModels, len(allowedURLModels) > 0
}

// checkDestinations returns an error if any of a channel URL's download destinations isn't available.
func checkDestinations(c *models.Channel, cu *models.ChannelURL) error {
	s := cmp.Or(cu.ChanURLSettings, c.ChanSettings)
	var dirs []string
	if s != nil {
		dirs = append(dirs, s.VideoDir, s.JSONDir)
		for _, op := range s.MetaFilterMoveOps {
			dirs = append(dirs, op.OutputDir)
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

// parsePipedVideoURL parses a video URL in the format "channelURL|videoURL".
func parsePipedVideoURL(videoURL string, channelURLMap map[string]*models.ChannelURL) (*models.ChannelURL, string, error) {
	parts := strings.Split(videoURL, "|")
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("invalid video URL format %q, expected 'channelURL|videoURL'", videoURL)
	}

	channelURLStr := parts[0]
	actualVideoURL := parts[1]

	targetChannelURLModel, found := channelURLMap[channelURLStr]
	if !found {
		return nil, "", fmt.Errorf("channel URL %q not found in channel's URL models", channelURLStr)
	}

	return targetChannelURLModel, actualVideoURL, nil
}

// parseManualVideoURL handles video URLs without a channel URL prefix.
func parseManualVideoURL(cs contracts.ChannelStore, c *models.Channel, videoURL string) (*models.ChannelURL, string, error) {
	if c == nil {
		return nil, "", fmt.Errorf("channel cannot be nil")
	}

	// Use special manual downloads entry.
	targetChannelURLModel, err := ensureManualDownloadsChannelURL(cs, c)
	if err != nil {
		return nil, "", err
	}
	return targetChannelURLModel, videoURL, nil
}
