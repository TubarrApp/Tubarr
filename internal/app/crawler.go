package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"tubarr/internal/cfg"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/scraper"
	"tubarr/internal/utils/logging"
	"tubarr/internal/validation"

	"golang.org/x/net/publicsuffix"
)

const (
	applicationJSON = "application/json"
)

// CheckChannels checks channels and whether they are due for a crawl.
func CheckChannels(s interfaces.Store, ctx context.Context) error {

	// Grab all channels from database
	cs := s.ChannelStore()
	channels, hasRows, err := cs.FetchAllChannels()
	if !hasRows {
		logging.I("No channels in database")
	} else if err != nil {
		return err
	}

	var (
		conc    = max(cfg.GetInt(keys.Concurrency), 1)
		errChan = make(chan error, len(channels))
		sem     = make(chan struct{}, conc)
		wg      sync.WaitGroup
	)

	// Iterate over channels
	for _, c := range channels {

		// Load in config file
		cfg.UpdateFromConfig(s.ChannelStore(), c)

		// Ignore channel if paused
		if c.ChanSettings.Paused {
			logging.I("Channel with name %q is paused, skipping checks.", c.Name)
			continue
		}

		// Check if site is blocked or should be unlocked
		if c.IsBlocked() {
			unlocked, err := cs.CheckAndUnlockChannel(c)
			if err != nil {
				logging.E("Failed to unlock channel %q, skipping due to error: %v", c.Name, err)
				return nil
			}

			if !unlocked {
				return nil
			}
		}

		// Time for a scan?
		timeSinceLastScan := time.Since(c.LastScan)
		crawlFreqDuration := time.Duration(c.ChanSettings.CrawlFreq) * time.Minute

		logging.I("\nTime since last check for channel %q: %s\nCrawl frequency: %d minutes",
			c.Name,
			timeSinceLastScan.Round(time.Second),
			c.ChanSettings.CrawlFreq)

		if timeSinceLastScan < crawlFreqDuration {
			remainingTime := crawlFreqDuration - timeSinceLastScan
			logging.P("Next check in: %s", remainingTime.Round(time.Second))
			fmt.Println()
			continue
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
			if err := ChannelCrawl(s, cs, c, ctx); err != nil {
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
		return fmt.Errorf("encountered %d errors during processing: %w", len(allErrs), errors.Join(allErrs...))
	}

	return nil
}

// DownloadVideosToChannel downloads custom video URLs sent in to the channel.
func DownloadVideosToChannel(s interfaces.Store, cs interfaces.ChannelStore, c *models.Channel, videoURLs []string, ctx context.Context) error {

	// Check if site is blocked or should be unlocked
	if c.IsBlocked() {
		unlocked, err := cs.CheckAndUnlockChannel(c)
		if err != nil {
			logging.E("Failed to unlock channel %q, skipping due to error: %v", c.Name, err)
			return nil
		}

		if !unlocked {
			return nil
		}
	}

	// Load config file settings
	cfg.UpdateFromConfig(s.ChannelStore(), c)

	// Grab existing releases
	scrape := scraper.New()
	existingVideoURLsMap, _, err := scrape.GetExistingReleases(cs, c)
	if err != nil {
		return err
	}

	// Populate AccessDetails for all ChannelURLs upfront
	for _, cu := range c.URLModels {
		cu.Cookies, cu.CookiePath, err = scrape.GetChannelCookies(cs, c, cu, ctx)
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
			targetChannelURLModel, actualVideoURL, err = parseManualVideoURL(cs, c, scrape, videoURL, ctx)
			if err != nil {
				return err
			}
		}

		// Check if already downloaded
		if _, exists := existingVideoURLsMap[actualVideoURL]; exists {
			return fmt.Errorf("video %q already downloaded to this channel, please delete it using 'delete-video-urls' first if you wish to re-download it", actualVideoURL)
		}

		// Store the model for later use
		channelURLModels[targetChannelURLModel.ID] = targetChannelURLModel

		customVideoRequests = append(customVideoRequests, &models.Video{
			ChannelID:    c.ID,
			ChannelURLID: targetChannelURLModel.ID,
			ChannelURL:   targetChannelURLModel.URL,
			URL:          actualVideoURL,
			Settings:     c.ChanSettings,
			MetarrArgs:   c.ChanMetarrArgs,
		})
	}

	// Retrieve existing URL directory map
	urlDirMap, err := validation.ValidateMetarrOutputDirs(c.ChanMetarrArgs.OutputDir, c.ChanMetarrArgs.URLOutputDirs, c)
	if err != nil {
		return err
	}

	// Fill output directories and batch channels together
	videosByChannelURL := make(map[int64][]*models.Video)
	for _, v := range customVideoRequests {
		if outputDir, exists := urlDirMap[v.ChannelURL]; exists {
			v.MetarrArgs.OutputDir = outputDir
		}
		videosByChannelURL[v.ChannelURLID] = append(videosByChannelURL[v.ChannelURLID], v)
	}

	// Main process
	var (
		nSucceeded     int
		procError      error
		channelsGotNew []string
	)

	// Process each ChannelURL's videos
	for channelURLID, videos := range videosByChannelURL {
		if len(videos) == 0 {
			continue
		}

		// Get the ChannelURL model we stored earlier
		cu, exists := channelURLModels[channelURLID]
		if !exists {
			logging.E("Could not find ChannelURL model for ID %d", channelURLID)
			continue
		}

		succeeded, nDownloaded, procErr := InitProcess(ctx, s, cu, c, scrape, videos)
		if succeeded != 0 {
			nSucceeded += succeeded
			if nDownloaded > 0 {
				channelsGotNew = append(channelsGotNew, cu.URL)
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

	if err := NotifyServices(cs, c, channelsGotNew); err != nil {
		return errors.Join(procError, err)
	}

	if procError != nil {
		return fmt.Errorf("encountered errors during processing: %w", procError)
	}

	return nil
}

// ChannelCrawl crawls a channel for new URLs.
func ChannelCrawl(s interfaces.Store, cs interfaces.ChannelStore, c *models.Channel, ctx context.Context) (err error) {

	// Check if site is blocked or should be unlocked
	if c.IsBlocked() {
		unlocked, err := cs.CheckAndUnlockChannel(c)
		if err != nil {
			logging.E("Failed to unlock channel %q, skipping due to error: %v", c.Name, err)
			return nil
		}

		if !unlocked {
			return nil
		}
	}

	// Proceed...
	fmt.Println()
	logging.I("%sINITIALIZING CRAWL:%s Channel %q:\n", consts.ColorGreen, consts.ColorReset, c.Name)

	// Check validity
	if len(c.URLModels) == 0 {
		return errors.New("no channel URLs")
	}
	if c.ChanSettings.VideoDir == "" || c.ChanSettings.JSONDir == "" {
		return errors.New("output directories are blank")
	}

	// Get new releases for channel
	scrape := scraper.New()
	videos, err := scrape.GetNewReleases(cs, c, ctx)
	if err != nil {
		return err
	}

	if len(videos) == 0 {
		logging.I("No new releases for channel %q", c.Name)

		if err := cs.UpdateLastScan(c.ID); err != nil {
			return fmt.Errorf("failed to update last scan time: %w", err)
		}

		// Return early, no new releases...
		return nil
	}

	// Main process...
	var (
		nSucceeded     int
		procError      error
		channelsGotNew []string
	)

	// Process channel URLs
	for _, cu := range c.URLModels {
		// Skip manual channel entry
		if cu.IsManual {
			continue
		}

		// Get requests for this channel
		var vRequests []*models.Video
		for _, v := range videos {
			if v.ChannelURL == cu.URL {
				vRequests = append(vRequests, v)
			}
		}
		if len(vRequests) == 0 {
			continue
		}
		logging.I("Got %d video(s) for URL %q %s(Channel: %s)%s", len(vRequests), cu.URL, consts.ColorGreen, c.Name, consts.ColorReset)

		// Process video batch
		succeeded, nDownloaded, procErr := InitProcess(ctx, s, cu, c, scrape, vRequests)

		// Succeeded/downloaded
		if succeeded != 0 {
			nSucceeded += succeeded

			if nDownloaded > 0 {
				logging.I("Successfully downloaded %d video(s) for URL %q %s(Channel: %s)%s", nDownloaded, cu.URL, consts.ColorGreen, c.Name, consts.ColorReset)
				channelsGotNew = append(channelsGotNew, cu.URL)
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
	if err := NotifyServices(cs, c, channelsGotNew); err != nil {
		return errors.Join(procError, err)
	}

	// Some errors encountered
	if procError != nil {
		return fmt.Errorf("encountered errors during processing: %w", procError)
	}

	return nil
}

// ChannelCrawlIgnoreNew gets the channel's currently displayed videos and marks them as complete without downloading.
func ChannelCrawlIgnoreNew(s interfaces.Store, c *models.Channel, ctx context.Context) error {

	cs := s.ChannelStore()

	// Check if site is blocked or should be unlocked
	if c.IsBlocked() {
		unlocked, err := cs.CheckAndUnlockChannel(c)
		if err != nil {
			logging.E("Failed to unlock channel %q, skipping due to error: %v", c.Name, err)
			return nil
		}

		if !unlocked {
			return nil
		}
	}

	// Load in config file
	cfg.UpdateFromConfig(cs, c)

	// Get new releases
	scrape := scraper.New()
	videos, err := scrape.GetNewReleases(cs, c, ctx)
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
			v.DownloadStatus.Status = consts.DLStatusCompleted
			v.DownloadStatus.Pct = 100.0
			v.Finished = true
		}

		validVideos, errArray := s.VideoStore().AddVideos(videos, c)
		if len(errArray) > 0 {
			logging.P("%s Encountered the following errors adding videos:", consts.RedError)
			for _, err := range errArray {
				logging.P("%v", err)
			}
			if len(validVideos) == 0 {
				return fmt.Errorf("no videos were successfully added to the ignore list for channel with ID %d", c.ID)
			}
		}

		logging.S(0, "Added %d videos to the ignore list in channel %q", len(validVideos), c.Name)
	}
	return nil
}

// ensureManualDownloadsChannelURL ensures a special "manual-downloads" ChannelURL exists for the channel.
func ensureManualDownloadsChannelURL(cs interfaces.ChannelStore, channelID int64) (*models.ChannelURL, error) {
	const manualDownloadsURL = "manual-downloads"

	// Check if it already exists
	channelURLs, err := cs.FetchChannelURLModels(channelID)
	if err != nil {
		return nil, err
	}

	for _, cu := range channelURLs {
		if cu.IsManual {
			return cu, nil
		}
	}

	manualChanURL := &models.ChannelURL{
		URL: manualDownloadsURL,
	}

	// Create it if it doesn't exist
	id, err := cs.AddChannelURL(channelID, manualChanURL, true) // true for is_manual
	if err != nil {
		return nil, fmt.Errorf("failed to create manual downloads channel URL: %w", err)
	}

	return &models.ChannelURL{
		ID:       id,
		URL:      manualDownloadsURL,
		IsManual: true,
	}, nil
}

// blockChannelBotDetected blocks a channel due to bot detection on the given URL.
func blockChannelBotDetected(cs interfaces.ChannelStore, c *models.Channel, cu *models.ChannelURL) error {
	parsedCURL, err := url.Parse(cu.URL)
	hostname := parsedCURL.Hostname()
	if err != nil {
		logging.E("Could not parse %q, will use full domain", cu.URL)
		hostname = cu.URL
	}

	// Extract the eTLD+1 (effective top-level domain + 1 label)
	// e.g., m.youtube.com -> youtube.com, www.bbc.co.uk -> bbc.co.uk
	if domain, err := publicsuffix.EffectiveTLDPlusOne(hostname); err == nil {
		hostname = strings.ToLower(domain)
	}

	_, err = cs.UpdateChannelSettingsJSON(consts.QChanID, strconv.FormatInt(c.ID, 10), func(s *models.ChannelSettings) error {
		s.BotBlocked = true
		s.BotBlockedHostname = hostname
		s.BotBlockedTimestamp = time.Now()
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
func parseManualVideoURL(cs interfaces.ChannelStore, c *models.Channel, scrape *scraper.Scraper, videoURL string, ctx context.Context) (*models.ChannelURL, string, error) {
	// Use special manual downloads entry
	targetChannelURLModel, err := ensureManualDownloadsChannelURL(cs, c.ID)
	if err != nil {
		return nil, "", err
	}

	// Get access details for manual downloads
	targetChannelURLModel.Cookies, targetChannelURLModel.CookiePath, err = scrape.GetChannelCookies(cs, c, targetChannelURLModel, ctx)
	if err != nil {
		return nil, "", err
	}

	return targetChannelURLModel, videoURL, nil
}
