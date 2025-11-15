package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"tubarr/internal/abstractions"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/file"
	"tubarr/internal/models"
	"tubarr/internal/scraper"
	"tubarr/internal/utils/logging"
	"tubarr/internal/utils/times"

	"golang.org/x/net/publicsuffix"
)

// CheckChannels checks channels and whether they are due for a crawl.
func CheckChannels(ctx context.Context, s contracts.Store) error {
	// Grab all channels from database
	cs := s.ChannelStore()
	channels, hasRows, err := cs.GetAllChannels(true)
	if !hasRows {
		logging.I("No channels in database")
	} else if err != nil {
		return err
	}

	var (
		conc    = max(abstractions.GetInt(keys.GlobalConcurrency), 1)
		errChan = make(chan error, len(channels))
		sem     = make(chan struct{}, conc)
		wg      sync.WaitGroup
	)

	// Iterate over channels
	for _, c := range channels {
		// Load in config file
		file.UpdateFromConfigFile(s.ChannelStore(), c)

		// Ignore channel if paused
		if c.ChanSettings.Paused {
			logging.I("Channel with name %q is paused, skipping checks.", c.Name)
			continue
		}

		// Time for a scan?
		crawlFreq := c.GetCrawlFreq()
		timeSinceLastScan := time.Since(c.LastScan)

		logging.I("Time since last check for channel %q: %s\nCrawl frequency: %d minutes",
			c.Name,
			timeSinceLastScan.Round(time.Second),
			crawlFreq)

		if crawlFreq > 0 {
			crawlFreqDuration := time.Duration(crawlFreq) * time.Minute
			if timeSinceLastScan < crawlFreqDuration {
				remainingTime := crawlFreqDuration - timeSinceLastScan
				logging.P("Next check in: %s", remainingTime.Round(time.Second))
				fmt.Println()
				continue
			}
		}

		// Run concurrent jobs
		wg.Add(1)
		go func(c *models.Channel) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() {
				<-sem
			}()

			// Initiate crawl
			if err := CrawlChannel(ctx, s, c); err != nil {
				errChan <- err
			}
		}(c)
	}

	wg.Wait()
	close(errChan)

	// Aggregate errors
	allErrs := make([]error, 0, len(channels))
	for e := range errChan {
		allErrs = append(allErrs, e)
	}

	if len(allErrs) > 0 {
		return errors.Join(allErrs...)
	}

	return nil
}

// DownloadVideosToChannel downloads custom video URLs sent in to the channel.
func DownloadVideosToChannel(ctx context.Context, s contracts.Store, cs contracts.ChannelStore, c *models.Channel, videoURLs []string) (err error) {
	if c == nil || c.ChanSettings == nil {
		return fmt.Errorf("invalid nil parameters (channel: %v, channel settings: %v)", c == nil, c.ChanSettings == nil)
	}

	// Add random sleep before processing (added bot detection)
	if err := times.WaitTime(ctx, times.RandomSecsDuration(consts.DefaultBotAvoidanceSeconds), c.Name, ""); err != nil {
		return err
	}

	// Check if site is blocked or should be unlocked
	unlocked := false
	if c.IsBlocked() {
		unlocked, err = cs.CheckOrUnlockChannel(c)
		if err != nil {
			logging.E("Failed to unlock channel %q, skipping due to error: %v", c.Name, err)
			return nil
		}

		// Filter out matching hostnames later.
	}

	// Load config file settings
	file.UpdateFromConfigFile(s.ChannelStore(), c)

	// Grab existing releases
	scrape := scraper.New()
	existingVideoURLsMap, _, err := scrape.GetExistingReleases(cs, c)
	if err != nil {
		return err
	}

	// Populate AccessDetails for all ChannelURLs upfront
	for _, cu := range c.URLModels {
		cu.Cookies, cu.CookiePath, err = scrape.GetChannelCookies(ctx, cs, c, cu)
		if err != nil {
			return err
		}
	}

	// Build a map of channel URL -> ChannelURL model for quick lookup
	channelURLMap := make(map[string]*models.ChannelURL, len(c.URLModels))
	for _, cu := range c.URLModels {
		channelURLMap[cu.URL] = cu
	}

	// Video request model slice
	customVideoRequests := make([]*models.Video, 0, len(videoURLs))

	// Track which ChannelURL model each video uses (by ChannelURLID)
	channelURLModels := make(map[int64]*models.ChannelURL, len(videoURLs))

	for _, videoURL := range videoURLs {
		var (
			targetChannelURLModel *models.ChannelURL
			actualVideoURL        string
		)

		// Parse pipe-delimited format: "channelURL|videoURL"
		if strings.Contains(videoURL, "|") {
			targetChannelURLModel, actualVideoURL, err = parsePipedVideoURL(videoURL, channelURLMap)
			if err != nil {
				return err
			}
		} else {
			targetChannelURLModel, actualVideoURL, err = parseManualVideoURL(ctx, cs, c, scrape, videoURL)
			if err != nil {
				return err
			}
		}

		if !unlocked {
			parsed, err := url.Parse(targetChannelURLModel.URL)
			if err != nil {
				logging.W("Unable to parse %q, skipping...", targetChannelURLModel.URL)
				continue
			}
			hostname := parsed.Hostname()
			if domain, err := publicsuffix.EffectiveTLDPlusOne(hostname); err == nil { // If err IS nil
				hostname = strings.ToLower(domain)
			}

			if slices.Contains(c.ChanSettings.BotBlockedHostnames, hostname) {
				continue // Hostname is blocked, skipping...
			}
		}

		// Check if already downloaded
		if _, exists := existingVideoURLsMap[actualVideoURL]; exists {
			return fmt.Errorf("video %q already downloaded to this channel, please delete it using 'delete-videos' first if you wish to re-download it", actualVideoURL)
		}

		// Store the model for later use
		channelURLModels[targetChannelURLModel.ID] = targetChannelURLModel

		customVideoRequests = append(customVideoRequests, &models.Video{
			ChannelID:    c.ID,
			ChannelURLID: targetChannelURLModel.ID,
			URL:          actualVideoURL,
		})
	}

	// Fill output directories and batch channels together
	videosByChannelURL := make(map[int64][]*models.Video)
	for _, v := range customVideoRequests {
		videosByChannelURL[v.ChannelURLID] = append(videosByChannelURL[v.ChannelURLID], v)
	}

	// Main process
	var (
		nSucceeded        int
		procError         error
		channelURLsGotNew []string
	)

	// Process each ChannelURL's videos
	for channelURLID, videos := range videosByChannelURL {
		if len(videos) == 0 {
			continue
		}

		// Get the ChannelURL model stored earlier
		cu, exists := channelURLModels[channelURLID]
		if !exists {
			logging.E("Could not find ChannelURL model for ID %d", channelURLID)
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

	// Update last scan time
	if err := cs.UpdateLastScan(c.ID); err != nil {
		return fmt.Errorf("failed to update last scan time: %w", err)
	}

	// Handle results
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

// CrawlChannel crawls a channel for new URLs.
func CrawlChannel(ctx context.Context, s contracts.Store, c *models.Channel) (err error) {
	if c == nil {
		return fmt.Errorf("channel cannot be nil")
	}
	cs := s.ChannelStore()

	// Add random sleep before processing (added bot detection)
	if err := times.WaitTime(ctx, times.RandomSecsDuration(consts.DefaultBotAvoidanceSeconds), c.Name, ""); err != nil {
		return err
	}

	// Check validity
	if len(c.URLModels) == 0 {
		return errors.New("no channel URLs")
	}
	if c.ChanSettings.VideoDir == "" || c.ChanSettings.JSONDir == "" {
		return errors.New("default channel output directories are blank")
	}

	fmt.Println()
	logging.I("%sINITIALIZING CRAWL:%s Channel %q:\n", consts.ColorGreen, consts.ColorReset, c.Name)

	// Check if site is blocked or should be unlocked, and filter URLs if needed
	if c.IsBlocked() {
		unlocked, err := cs.CheckOrUnlockChannel(c)
		if err != nil {
			logging.E("Failed to unlock channel %q, skipping due to error: %v", c.Name, err)
			return nil
		}

		// If still blocked, filter out URLs from blocked hostname
		if !unlocked {
			allowedURLModels, hasAllowed := filterBlockedURLs(c)

			if !hasAllowed {
				logging.D(1, "All URLs for channel %q are blocked by hostname(s) %v, skipping crawl", c.Name, c.ChanSettings.BotBlockedHostnames)
				return nil
			}

			// Replace with filtered list
			c.URLModels = allowedURLModels
		}
	}

	// Get new releases for channel
	scrape := scraper.New()
	videos, err := scrape.GetNewReleases(ctx, cs, c)
	if err != nil {
		return err
	}

	if len(videos) == 0 {
		logging.I("No new releases for channel %q", c.Name)
		if err := cs.UpdateLastScan(c.ID); err != nil {
			return fmt.Errorf("failed to update last scan time: %w", err)
		}
		return nil
	}

	// Main process...
	var (
		nSucceeded,
		nDownloaded int
		procError         error
		channelURLsGotNew []string
	)

	// Process channel URLs
	for _, cu := range c.URLModels {
		// Skip manual channel entry
		if cu.IsManual || cu.ChanURLSettings.Paused {
			continue
		}

		// Get requests for this channel
		var vRequests []*models.Video
		for _, v := range videos {
			if v.ChannelURLID == cu.ID {
				vRequests = append(vRequests, v)
			}
		}
		if len(vRequests) == 0 {
			continue
		}

		logging.I("Got %d video(s) for URL %q %s(Channel: %s)%s", len(vRequests), cu.URL, consts.ColorGreen, c.Name, consts.ColorReset)

		// Process video batch
		succeeded, downloaded, procErr := InitProcess(ctx, s, c, cu, vRequests, scrape)

		// Succeeded/downloaded
		if succeeded != 0 {
			nSucceeded += succeeded
			if downloaded > 0 {
				logging.S("Successfully downloaded %d video(s) for URL %q %s(Channel: %s)%s", downloaded, cu.URL, consts.ColorGreen, c.Name, consts.ColorReset)
				channelURLsGotNew = append(channelURLsGotNew, cu.URL)
				nDownloaded += downloaded
			}
		}
		procError = errors.Join(procError, procErr)
	}

	// Last scan time update
	if err := cs.UpdateLastScan(c.ID); err != nil {
		return fmt.Errorf("failed to update last scan time: %w", err)
	}

	// All videos failed
	if nSucceeded == 0 {
		return fmt.Errorf("failed to process %d video downloads. Got errors: %w", len(videos), procError)
	}

	// Some succeeded, notify URLs
	if nDownloaded > 0 {
		if err := NotifyServices(cs, c, channelURLsGotNew); err != nil {
			return errors.Join(procError, err)
		}
	}

	// Some errors encountered
	if procError != nil {
		return fmt.Errorf("channel %q encountered errors processing: %w", c.Name, procError)
	}

	return nil
}

// CrawlChannelIgnore gets the channel's currently displayed videos and marks them as complete without downloading.
func CrawlChannelIgnore(ctx context.Context, s contracts.Store, c *models.Channel) error {
	if c == nil {
		return fmt.Errorf("channel cannot be nil")
	}
	cs := s.ChannelStore()

	// Check if site is blocked or should be unlocked
	if c.IsBlocked() {
		unlocked, err := cs.CheckOrUnlockChannel(c)
		if err != nil {
			logging.E("Failed to unlock channel %q, skipping due to error: %v", c.Name, err)
			return nil
		}

		if !unlocked {
			return nil
		}
	}

	// Load in config file
	file.UpdateFromConfigFile(cs, c)

	// Get new releases
	scrape := scraper.New()
	videos, err := scrape.GetNewReleases(ctx, cs, c)
	if err != nil {
		return err
	}

	// Add videos to ignore
	if len(videos) > 0 {
		for _, v := range videos {
			if v.URL == "" {
				logging.D(5, "Skipping invalid video entry with no URL in channel %q", c.Name)
				continue
			}
			v.MarkVideoAsIgnored()
		}

		validVideos, err := s.VideoStore().AddVideos(videos, c.ID)
		if err != nil {
			logging.E("Encountered the following errors adding videos for channel %q: %v", c.Name, err)

			if len(validVideos) == 0 {
				return fmt.Errorf("no videos were successfully added to the ignore list for channel with ID %d", c.ID)
			}
		}

		logging.S("Added %d videos to the ignore list in channel %q", len(validVideos), c.Name)
	}
	return nil
}

// filterBlockedURLs filters out URLs blocked by the hostname of a given channel URL.
func filterBlockedURLs(c *models.Channel) ([]*models.ChannelURL, bool) {

	allowedURLModels := make([]*models.ChannelURL, 0, len(c.URLModels))
	for _, cu := range c.URLModels {
		if cu.IsManual {
			continue
		}

		parsed, err := url.Parse(cu.URL)
		if err != nil {
			logging.W("Unable to parse %q, skipping...", cu.URL)
			continue
		}

		hostname := parsed.Hostname()
		if domain, err := publicsuffix.EffectiveTLDPlusOne(hostname); err == nil { // If err IS nil
			hostname = strings.ToLower(domain)
		}

		if slices.Contains(c.ChanSettings.BotBlockedHostnames, hostname) {
			logging.D(1, "Skipping URL %q for channel %q. (Blocked by %q)", cu.URL, c.Name, hostname)
			continue
		}

		// Hostname doesn't match the blocked site, safe to proceed
		allowedURLModels = append(allowedURLModels, cu)
	}

	return allowedURLModels, len(allowedURLModels) > 0
}

// ensureManualDownloadsChannelURL ensures a special "manual-downloads" ChannelURL exists for the channel.
func ensureManualDownloadsChannelURL(cs contracts.ChannelStore, c *models.Channel) (*models.ChannelURL, error) {
	// First check in-memory models
	for _, cu := range c.URLModels {
		if cu.IsManual {
			return cu, nil // Return existing model
		}
	}

	// If not found in memory, check database directly
	// This handles cases where the channel was loaded before the manual entry was created
	existingCU, hasRows, err := cs.GetChannelURLModel(c.ID, consts.ManualDownloadsCol, true)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing manual downloads URL: %w", err)
	}
	if hasRows && existingCU != nil {
		c.URLModels = append(c.URLModels, existingCU)
		return existingCU, nil
	}

	// Get channel to inherit settings
	manualChanURL := &models.ChannelURL{
		URL:               consts.ManualDownloadsCol,
		ChanURLSettings:   c.ChanSettings,
		ChanURLMetarrArgs: c.ChanMetarrArgs,
	}

	// Create it if it doesn't exist
	id, err := cs.AddChannelURL(c.ID, manualChanURL, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create manual downloads channel URL: %w", err)
	}

	manualChanURL.ID = id
	manualChanURL.IsManual = true

	// Add to in-memory models
	c.URLModels = append(c.URLModels, manualChanURL)

	return manualChanURL, nil
}

// blockChannelBotDetected blocks a channel due to bot detection on the given URL.
func blockChannelBotDetected(cs contracts.ChannelStore, c *models.Channel, cu *models.ChannelURL) error {
	parsedCURL, err := url.Parse(cu.URL)
	hostname := parsedCURL.Hostname()
	if err != nil {
		logging.E("Could not parse %q, will use full domain", cu.URL)
		hostname = cu.URL
	}

	// Extract the eTLD+1 (effective top-level domain + 1 label)
	// e.g., m.google.com -> google.com, www.bbc.co.uk -> bbc.co.uk
	if domain, err := publicsuffix.EffectiveTLDPlusOne(hostname); err == nil { // If err IS nil
		hostname = strings.ToLower(domain)
	}

	_, err = cs.UpdateChannelSettingsJSON(consts.QChanID, strconv.FormatInt(c.ID, 10), func(s *models.Settings) error {
		s.BotBlocked = true

		// Add hostname if not already in the list
		if !slices.Contains(s.BotBlockedHostnames, hostname) {
			s.BotBlockedHostnames = append(s.BotBlockedHostnames, hostname)
		}

		// Initialize map if nil
		if s.BotBlockedTimestamps == nil {
			s.BotBlockedTimestamps = make(map[string]time.Time)
		}
		s.BotBlockedTimestamps[hostname] = time.Now()
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to block channel %q due to bot detection: %w", c.Name, err)
	}

	logging.W("Blocked channel %q due to bot detection on hostname %q", c.Name, hostname)
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
func parseManualVideoURL(ctx context.Context, cs contracts.ChannelStore, c *models.Channel, scrape *scraper.Scraper, videoURL string) (*models.ChannelURL, string, error) {
	if c == nil {
		return nil, "", fmt.Errorf("channel cannot be nil")
	}

	// Use special manual downloads entry
	targetChannelURLModel, err := ensureManualDownloadsChannelURL(cs, c)
	if err != nil {
		return nil, "", err
	}

	// Get access details for manual downloads
	targetChannelURLModel.Cookies, targetChannelURLModel.CookiePath, err = scrape.GetChannelCookies(ctx, cs, c, targetChannelURLModel)
	if err != nil {
		return nil, "", err
	}

	return targetChannelURLModel, videoURL, nil
}
