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
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/progflags"
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

// CrawlIgnoreNew gets the channel's currently displayed videos and ignores them on subsequent crawls.
//
// Essentially it marks the URLs it finds as though they have already been downloaded.
func CrawlIgnoreNew(s interfaces.Store, c *models.Channel, ctx context.Context) error {
	videos, err := browserInstance.GetNewReleases(s.ChannelStore(), c, ctx)
	if err != nil {
		return err
	}

	if len(videos) > 0 {
		for _, v := range videos {
			if v.URL == "" {
				logging.D(5, "Skipping invalid video entry with no URL in channel %q", c.URL)
				continue
			}
			v.DownloadStatus.Status = consts.DLStatusCompleted
			v.DownloadStatus.Pct = 100.0
		}

		validVideos, errArray := s.VideoStore().AddVideos(videos, c)
		if len(errArray) > 0 {
			logging.P("%s Encountered the following errors adding videos:", consts.RedError)
			fmt.Println()
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

// CheckChannels checks channels and whether they are due for a crawl.
func CheckChannels(s interfaces.Store, ctx context.Context) error {
	cs := s.ChannelStore()
	chans, err, hasRows := cs.FetchAllChannels()
	if !hasRows {
		logging.I("No channels in database")
	} else if err != nil {
		return err
	}

	var (
		wg sync.WaitGroup
	)

	conc := cfg.GetInt(keys.Concurrency)
	if conc < 1 {
		conc = 1
	}

	sem := make(chan struct{}, conc)
	errChan := make(chan error, len(chans))

	for i := range chans {

		if chans[i].Paused {
			logging.I("Channel with name %q is paused, skipping checks.", chans[i].Name)
			continue
		}

		timeSinceLastScan := time.Since(chans[i].LastScan)
		crawlFreqDuration := time.Duration(chans[i].Settings.CrawlFreq) * time.Minute

		fmt.Println()
		logging.I("Time since last check for channel %q: %s\nCrawl frequency: %d minutes",
			chans[i].Name,
			timeSinceLastScan.Round(time.Second),
			chans[i].Settings.CrawlFreq)

		if timeSinceLastScan < crawlFreqDuration {
			remainingTime := crawlFreqDuration - timeSinceLastScan
			logging.P("Next check in: %s", remainingTime.Round(time.Second))
			fmt.Println()
			continue
		}

		wg.Add(1)
		go func(c *models.Channel) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() {
				<-sem
			}()

			if err := ChannelCrawl(s, c, ctx); err != nil {
				errChan <- err
			}
		}(chans[i])
	}

	wg.Wait()
	close(errChan)

	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("encountered %d errors during processing: %v", len(errors), errors)
	}

	return nil
}

// ChannelCrawl crawls a channel for new URLs.
func ChannelCrawl(s interfaces.Store, c *models.Channel, ctx context.Context) error {
	const (
		errMsg = "encountered %d errors during processing: %v"
	)

	logging.I("Initiating crawl for URL %s...\n\nVideo destination: %s\nJSON destination: %s\nFilters: %v\nCookies source: %s",
		c.URL, c.VideoDir, c.JSONDir, c.Settings.Filters, c.Settings.CookieSource)

	switch {
	case c.URL == "":
		return errors.New("channel URL is blank")
	case c.VideoDir == "", c.JSONDir == "":
		return errors.New("output directories are blank")
	}

	cs := s.ChannelStore()

	// Parse output directories
	dirParser := parsing.NewDirectoryParser(c, nil)
	videoDir, err := dirParser.ParseDirectory(c.VideoDir)
	if err != nil {
		return fmt.Errorf("failed to parse video directory: %w", err)
	}

	jsonDir, err := dirParser.ParseDirectory(c.JSONDir)
	if err != nil {
		return fmt.Errorf("failed to parse JSON directory: %w", err)
	}

	c.VideoDir = videoDir
	c.JSONDir = jsonDir

	logging.S(1, "Parsed video directory: %s", c.VideoDir)
	logging.S(1, "Parsed JSON directory: %s", c.JSONDir)

	videos, err := browserInstance.GetNewReleases(cs, c, ctx)
	if err != nil {
		return err
	}

	// Check for custom scraper needs
	for _, v := range videos {

		// Detect censored.tv links
		if strings.Contains(c.URL, "censored.tv") {
			if !progflags.CensoredTVUseCustom {
				logging.I("Using regular censored.tv scraper...")
			} else {
				logging.I("Detected a censored.tv link. Using specialized scraper.")
				err := browserInstance.ScrapeCensoredTVMetadata(v.URL, c.JSONDir, v)
				if err != nil {
					return fmt.Errorf("failed to scrape censored.tv metadata: %w", err)
				}
			}
		}
	}

	var (
		success  bool
		errArray []error
	)

	if len(videos) == 0 {
		logging.I("No new releases for channel %q", c.URL)
		return nil
	} else {
		success, errArray = InitProcess(s, c, videos, ctx)
		if errArray != nil {
			logging.AddToErrorArray(err)
		}

		if err := cs.UpdateLastScan(c.ID); err != nil {
			return fmt.Errorf("failed to update last scan time: %w", err)
		}

		if !success {
			return fmt.Errorf(errMsg, len(errArray), errArray)
		}
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
			var b strings.Builder
			totalLength := 0
			for _, err := range errs {
				totalLength += len(err.Error())
			}
			b.Grow(totalLength + (len(errs)-1)*2)

			for i, err := range errs {
				b.WriteString(err.Error())
				if i != len(errs)-1 {
					b.WriteString(", ")
				}

			}
			return fmt.Errorf("errors sending notifications for channel with ID %d:\n%s", c.ID, b.String())
		}
	}

	if len(errArray) > 0 {
		return fmt.Errorf(errMsg, len(errArray), errArray)
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
