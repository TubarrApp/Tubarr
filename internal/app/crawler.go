// Package app crawls channels for new videos, and keeps their Available lists.
package app

import (
	"context"
	"errors"
	"fmt"
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
	"tubarr/internal/metadata"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/queue"
	"tubarr/internal/scraper"
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
func CheckChannels(ctx context.Context, s contracts.Store, dq *queue.DownloadQueue) error {
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
		wg.Go(func() {
			sem <- struct{}{}
			defer func() {
				<-sem
			}()

			// Initiate crawl.
			if performedCrawls, err := CrawlChannel(ctx, s, dq, &ch); err != nil {
				errChan <- err
			} else if performedCrawls {
				logger.Pl.S("Completed crawl for channel %q", ch.Name)
			}
		})
	}

	wg.Wait()
	close(errChan)

	allErrs := make([]error, 0, len(channels))
	for e := range errChan {
		allErrs = append(allErrs, e)
	}
	return errors.Join(allErrs...)
}

// ManualRequests returns the download requests for video URLs the user sent in.
func ManualRequests(manualVideos []models.ManualVideo) []queue.Request {
	reqs := make([]queue.Request, 0, len(manualVideos))
	for _, mv := range manualVideos {
		v := &models.Video{URL: mv.URL, ChannelURLID: mv.ChannelURLID, Title: mv.Title}
		reqs = append(reqs, queue.Request{Video: v, ByUser: true, Auth: mv.Auth})
	}
	return reqs
}

// AvailableRequests returns the download requests for videos the user chose from Available, using the login details
// given (or nil) for this download only.
//
// With replaceOverrides, overrides replace the videos' stored Download As options, nil clearing them.
func AvailableRequests(videoIDs []int64, auth *models.ManualAuth, replaceOverrides bool, overrides *models.VideoOverrides) []queue.Request {
	reqs := make([]queue.Request, 0, len(videoIDs))
	for _, id := range videoIDs {
		reqs = append(reqs, queue.Request{
			Video:            &models.Video{ID: id},
			ByUser:           true,
			Auth:             auth,
			ReplaceOverrides: replaceOverrides,
			Overrides:        overrides,
		})
	}
	return reqs
}

// UnavailableDestinations returns why videos can't be downloaded now, as their channel URL's destination is unavailable
// (e.g. an unmounted drive).
func UnavailableDestinations(c *models.Channel, manualVideos []models.ManualVideo) (bad []string) {
	for _, mv := range manualVideos {
		cu := &models.ChannelURL{} // Manual downloads use the channel's settings.
		if found := c.URLByID(mv.ChannelURLID); mv.ChannelURLID != 0 && found != nil {
			cu = found
		}
		if err := queue.CheckDestinations(c, cu, nil); err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", mv.URL, err))
		}
	}
	return bad
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

// CrawlChannel crawls a channel for new URLs, adding them to its Available list or the download queue.
func CrawlChannel(ctx context.Context, s contracts.Store, dq *queue.DownloadQueue, c *models.Channel) (performedCrawls bool, err error) {
	if c == nil {
		return false, errors.New("channel cannot be nil")
	}
	if c.LoadErr != nil {
		return false, c.LoadErr
	}
	if len(c.URLModels) == 0 {
		return false, nil
	}
	AddRunningProcess()
	defer SubtractRunningProcess()

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

	// Add random sleep before processing (added bot detection).
	if err := times.WaitTime(ctx, times.RandomSecsDuration(consts.DefaultBotAvoidanceSeconds), c.Name, "", ""); err != nil {
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
	videos, crawlErr := scrape.GetNewReleases(ctx, cs, c, false)
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
		nQueued, nAvailable int
		procError           = crawlErr
	)

	// Group videos by channel URL.
	videosByURL := make(map[int64][]*models.Video, len(c.URLModels))
	for _, v := range videos {
		videosByURL[v.ChannelURLID] = append(videosByURL[v.ChannelURLID], v)
	}

	// Process channel URLs.
	for _, cu := range c.URLModels {
		// Skip manual or paused channel entry.
		if cu.IsManual || cu.Paused() {
			continue
		}

		vRequests := videosByURL[cu.ID]
		if len(vRequests) == 0 {
			continue
		}
		if ctx.Err() != nil {
			procError = errors.Join(procError, ctx.Err())
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

		// Queue the videos for download.
		reqs, err := crawlRequests(c, cu, vRequests)
		if err != nil {
			procError = errors.Join(procError, err)
			continue
		}
		queued, err := dq.Add(c, reqs...)
		procError = errors.Join(procError, err)
		logger.Pl.S("Queued %d video(s) for URL %q %s(Channel: %s)%s", len(queued), cu.URL, sharedconsts.ColorGreen, c.Name, sharedconsts.ColorReset)
		nQueued += len(queued)
	}

	// Last scan time update.
	if err := markScanned(); err != nil {
		return true, fmt.Errorf("failed to update last scan time: %w", err)
	}

	// All videos failed.
	if nQueued == 0 && nAvailable == 0 && procError != nil {
		return true, fmt.Errorf("failed to process %d video downloads. Got errors: %w", len(videos), procError)
	}

	// Some errors encountered.
	if procError != nil {
		return true, fmt.Errorf("channel %q encountered errors processing: %w", c.Name, procError)
	}

	return true, nil
}

// crawlRequests returns the download requests for a channel URL's crawled videos.
//
// An unusable filter file queues none of them, so nothing downloads unfiltered.
func crawlRequests(c *models.Channel, cu *models.ChannelURL, videos []*models.Video) ([]queue.Request, error) {
	if _, err := metadata.LoadFilterOpsFromFile(cu, parsing.NewDirectoryParser(c, cu, parsing.TubarrTags)); err != nil {
		logger.Pl.E("Not downloading crawled videos for %q until its filter file is fixed: %v", cu.URL, err)
		return nil, fmt.Errorf("channel URL %q: %w", cu.URL, err)
	}
	reqs := make([]queue.Request, 0, len(videos))
	for _, v := range videos {
		reqs = append(reqs, queue.Request{Video: v})
	}
	return reqs, nil
}

// CrawlChannelIgnore gets the channel's currently displayed videos and marks them as complete without downloading.
func CrawlChannelIgnore(ctx context.Context, s contracts.Store, c *models.Channel) error {
	if c == nil {
		return errors.New("channel cannot be nil")
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
	videos, crawlErr := scrape.GetNewReleases(ctx, cs, c, true)
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

		// Check if this URL is blocked globally for its authentication context.
		if err := queue.DomainBlocked(cu); err != nil {
			logger.Pl.D(2, "Skipping URL %q for channel %q: %v", cu.URL, c.Name, err)
			continue
		}

		// Don't download anywhere unexpected (e.g. onto the local disk under an unmounted drive's mount point).
		if err := queue.CheckDestinations(c, cu, nil); err != nil {
			logger.Pl.E("Skipping URL %q for channel %q: %v", cu.URL, c.Name, err)
			continue
		}

		// Hostname not blocked, safe to proceed.
		allowedURLModels = append(allowedURLModels, cu)
	}
	return allowedURLModels, len(allowedURLModels) > 0
}

// CheckManualVideos returns the videos that can't be sent in to the channel, with why. Each must be a full http(s) video
// URL, with no channel URL or one of the channel's.
func CheckManualVideos(c *models.Channel, manualVideos []models.ManualVideo) (bad []string) {
	for _, mv := range manualVideos {
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

// AddManualAvailable adds video URLs to the channel's Available list, returning the added videos and the URLs already
// in the channel.
func AddManualAvailable(s contracts.Store, c *models.Channel, manualVideos []models.ManualVideo) (added []*models.Video, existing []string, err error) {
	if bad := CheckManualVideos(c, manualVideos); len(bad) > 0 {
		return nil, nil, fmt.Errorf("can't add:\n%s", strings.Join(bad, "\n"))
	}
	requests := make([]*models.Video, 0, len(manualVideos))
	for _, mv := range manualVideos {
		if slices.ContainsFunc(requests, func(v *models.Video) bool { return v.URL == mv.URL }) {
			continue
		}
		cu, err := queue.ManualChannelURL(s.ChannelStore(), c, mv.ChannelURLID)
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
		if notifications, err = parsing.ParseNotifications("", *input.Notification); err != nil {
			return nil, fmt.Errorf("%w: invalid notification URLs: %w", contracts.ErrInvalidChannel, err)
		}
	}
	logger.Pl.I("Adding channel %q to Tubarr...", c.Name)
	if err := cs.AddChannelWithDetails(c, authMap, notifications); err != nil {
		return nil, err
	}
	return c, nil
}
