package process

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"tubarr/internal/cfg"
	cfgchannel "tubarr/internal/cfg/channel"
	"tubarr/internal/cfg/validation"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/utils/browser"
	"tubarr/internal/utils/logging"
)

var (
	regClient       *http.Client
	lanClient       *http.Client
	initClientsOnce sync.Once
	browserInstance *browser.Browser
)

const (
	applicationJSON = "application/json"
)

func init() {
	browserInstance = browser.NewBrowser()
}

// initClients initializes HTTP clients for web activities.
func initClients() {
	initClientsOnce.Do(func() {
		regClient = &http.Client{
			Timeout: 10 * time.Second,
		}
		lanClient = &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true, // Skip SSL verification for self-hosted servers
				},
			},
		}
		logging.D(2, "HTTP clients initialized")
	})
}

// CheckChannels checks channels and whether they are due for a crawl.
func CheckChannels(s interfaces.Store, ctx context.Context) error {

	// Grab all channels from database
	cs := s.ChannelStore()
	channels, err, hasRows := cs.FetchAllChannels()
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
		cfgchannel.LoadFromConfig(s.ChannelStore(), c)

		// Ignore channel if paused
		if c.Settings.Paused {
			logging.I("Channel with name %q is paused, skipping checks.", c.Name)
			continue
		}

		// Time for a scan?
		timeSinceLastScan := time.Since(c.LastScan)
		crawlFreqDuration := time.Duration(c.Settings.CrawlFreq) * time.Minute

		logging.I("\nTime since last check for channel %q: %s\nCrawl frequency: %d minutes",
			c.Name,
			timeSinceLastScan.Round(time.Second),
			c.Settings.CrawlFreq)

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
	var allErrs []error
	for e := range errChan {
		allErrs = append(allErrs, e)
	}

	if len(allErrs) > 0 {
		return fmt.Errorf("encountered %d errors during processing: %w", len(allErrs), errors.Join(allErrs...))
	}

	return nil
}

// ChannelCrawl crawls a channel for new URLs.
func ChannelCrawl(s interfaces.Store, cs interfaces.ChannelStore, c *models.Channel, ctx context.Context) (err error) {

	logging.I("Initiating crawl for channel %q...\n\nVideo destination: %s\nJSON destination: %s\nFilters: %v\nCookies source: %s",
		c.Name, c.Settings.VideoDir, c.Settings.JSONDir, c.Settings.Filters, c.Settings.CookieSource)

	// Check validity
	if len(c.URLs) == 0 {
		return errors.New("no channel URLs")
	}
	if c.Settings.VideoDir == "" || c.Settings.JSONDir == "" {
		return errors.New("output directories are blank")
	}

	videos, err := browserInstance.GetNewReleases(cs, c, ctx)
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

	// Check for custom scraper needs
	if err := checkCustomScraperNeeds(videos, c, cs); err != nil {
		return err
	}

	// Main process
	success, errArray := InitProcess(s, c, videos, ctx)

	// Last scan time update
	if err := cs.UpdateLastScan(c.ID); err != nil {
		return fmt.Errorf("failed to update last scan time: %w", err)
	}

	// Add errors to array on failure
	if !success {
		return fmt.Errorf("failed to process video downloads. Got errors: %v", errArray)
	}

	// Some successful downloads, notify URLs
	notifyURLs, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return err
		}
		logging.D(1, "No notification URL for channel with name %q and ID: %d", c.Name, c.ID)
	}

	if len(notifyURLs) > 0 {
		if errs := notify(c, notifyURLs); len(errs) != 0 {
			return fmt.Errorf("errors sending notifications for channel with ID %d:\n%s", c.ID, errors.Join(errs...))
		}
	}

	if errArray != nil {
		return fmt.Errorf("encountered errors during processing: %v", errArray)
	}

	return nil
}

// ChannelCrawlIgnoreNew gets the channel's currently displayed videos and ignores them on subsequent crawls.
//
// Essentially it marks the URLs it finds as though they have already been downloaded.
func ChannelCrawlIgnoreNew(s interfaces.Store, c *models.Channel, ctx context.Context) error {

	// Load in config file
	cfgchannel.LoadFromConfig(s.ChannelStore(), c)

	// Get new releases
	videos, err := browserInstance.GetNewReleases(s.ChannelStore(), c, ctx)
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

// DownloadVideosToChannel downloads custom video URLs sent in to the channel.
func DownloadVideosToChannel(s interfaces.Store, cs interfaces.ChannelStore, c *models.Channel, videoURLs []string, ctx context.Context) error {

	// Load in config file
	cfgchannel.LoadFromConfig(s.ChannelStore(), c)

	// Load already downloaded URLs
	existingVideoURLsMap, _, err := browserInstance.GetExistingReleases(cs, c)
	if err != nil {
		return err
	}

	customVideoRequests := []*models.Video{}

	for _, channelURL := range c.URLs {
		for _, videoURL := range videoURLs {

			var (
				customVideoChanURL string
				customVideoURL     = videoURL
			)

			if strings.Contains(videoURL, "|") {
				split := strings.Split(videoURL, "|")
				customVideoChanURL = split[0]
				customVideoURL = split[1]
			}

			if _, exists := existingVideoURLsMap[customVideoURL]; exists {
				return fmt.Errorf("video %q already downloaded to this channel, please delete it using 'delete-video-urls' first if you wish to re-download it", customVideoURL)
			}

			_, chanAccessDetails, err := browserInstance.GetChannelAccessDetails(cs, c, channelURL)
			if err != nil {
				return err
			}

			customVideoRequests = append(customVideoRequests, &models.Video{
				ChannelID:  c.ID,
				URL:        customVideoURL,
				ChannelURL: customVideoChanURL,
				Channel:    c,
				Settings:   c.Settings,
				MetarrArgs: c.MetarrArgs,
				CookiePath: chanAccessDetails.CookiePath,
			})
		}
	}

	// Retrieve existing URL directory map.
	urlDirMap, err := validation.ValidateMetarrOutputDirs(c.MetarrArgs.OutputDir, c.MetarrArgs.URLOutputDirs, c)
	if err != nil {
		return err
	}

	// Fill output directory from map where applicable.
	for range urlDirMap {
		for _, v := range customVideoRequests {
			if v.ChannelURL == "" {
				continue
			}
			if _, exists := urlDirMap[v.ChannelURL]; exists {
				v.MetarrArgs.OutputDir = urlDirMap[v.ChannelURL]
			}
		}
	}

	// Main process
	success, errArray := InitProcess(s, c, customVideoRequests, ctx)

	// Last scan time update
	if err := cs.UpdateLastScan(c.ID); err != nil {
		return fmt.Errorf("failed to update last scan time: %w", err)
	}

	// Add errors to array on failure
	if !success {
		return fmt.Errorf("failed to process video downloads. Got errors: %v", errArray)
	}

	// Some successful downloads, notify URLs
	notifyURLs, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return err
		}
		logging.D(1, "No notification URL for channel with name %q and ID: %d", c.Name, c.ID)
	}

	if len(notifyURLs) > 0 {
		if errs := notify(c, notifyURLs); len(errs) != 0 {
			return fmt.Errorf("errors sending notifications for channel with ID %d:\n%s", c.ID, errors.Join(errs...))
		}
	}

	if errArray != nil {
		return fmt.Errorf("encountered errors during processing: %v", errArray)
	}

	return nil
}

// notify pings notification services as required.
func notify(c *models.Channel, notifyURLs []string) []error {

	// Setup clients
	initClients()

	// Inner function
	notifyFunc := func(client *http.Client, notifyURL string) error {
		resp, err := client.Post(notifyURL, applicationJSON, nil)
		if err != nil {
			return fmt.Errorf("failed to send notification to URL %q for channel %q (ID: %d): %w",
				notifyURL, c.Name, c.ID, err)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				logging.E(0, "Failed to close HTTP response body: %v", err)
			}
		}()

		if resp.StatusCode >= 400 {
			return fmt.Errorf("notification failed with status %d for channel %q (ID: %d)", resp.StatusCode, c.Name, c.ID)
		}
		return nil
	}

	// Notify for each URL
	errs := make([]error, 0, len(notifyURLs))

	for _, notifyURL := range notifyURLs {
		parsed, err := url.Parse(notifyURL)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid notification URL %q: %w", notifyURL, err))
			continue
		}

		client := regClient
		if isPrivateNetwork(parsed.Host) {
			client = lanClient
		}

		if err := notifyFunc(client, notifyURL); err != nil {
			errs = append(errs, fmt.Errorf("failed to notify URL %q: %w", notifyURL, err))
			continue
		}
		logging.S(1, "Successfully notified URL %q for channel %q", notifyURL, c.Name)
	}

	if len(errs) == 0 {
		logging.S(0, "Successfully notified all URLs for channel %q: %v", c.Name, notifyURLs)
		return nil
	}

	return errs
}
