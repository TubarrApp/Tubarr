package process

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
	"tubarr/internal/cfg"
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
)

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

// CrawlIgnoreNew gets the channel's currently shown videos and adds to ignore on subsequent crawls
func CrawlIgnoreNew(s interfaces.Store, c *models.Channel) error {
	cs := s.GetChannelStore()
	videos, err := browser.GetNewReleases(cs, c)
	if err != nil {
		return err
	}

	if len(videos) > 0 {
		count := 0
		vs := s.GetVideoStore()

		for _, v := range videos {
			v.Downloaded = true
			if _, err := vs.AddVideo(v); err != nil {
				logging.E(0, err.Error())
			}

			logging.S(0, "Added URL '%s' to ignore list.", v.URL)
			count++
		}
	}
	return nil
}

// CheckChannels checks channels and whether they are due for a crawl
func CheckChannels(s interfaces.Store) error {
	cs := s.GetChannelStore()
	channels, err, hasRows := cs.ListChannels()
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
	errChan := make(chan error, len(channels))

	for _, channel := range channels {

		timeSinceLastScan := time.Since(channel.LastScan)
		crawlFreqDuration := time.Duration(channel.Settings.CrawlFreq) * time.Minute

		logging.I("Time since last check for channel '%s': %s\nCrawl frequency: %d minutes",
			channel.Name,
			timeSinceLastScan.Round(time.Second),
			channel.Settings.CrawlFreq)

		if timeSinceLastScan < crawlFreqDuration {
			remainingTime := crawlFreqDuration - timeSinceLastScan
			logging.I("Next check in: %s", remainingTime.Round(time.Second))
			continue
		}

		wg.Add(1)
		go func(c models.Channel) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() {
				<-sem
			}()

			if err := ChannelCrawl(s, c); err != nil {
				errChan <- err
			}
		}(channel)
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

// ChannelCrawl crawls a channel for new URLs
func ChannelCrawl(s interfaces.Store, c models.Channel) error {
	logging.I("Initiating crawl for URL %s...\n\nVideo destination: %s\nJSON destination: %s\nFilters: %v\nCookies source: %s",
		c.URL, c.VDir, c.JDir, c.Settings.Filters, c.Settings.CookieSource)

	switch {
	case c.URL == "":
		return fmt.Errorf("channel URL is blank")
	case c.VDir == "", c.JDir == "":
		return fmt.Errorf("output directories are blank")
	}

	cs := s.GetChannelStore()

	videos, err := browser.GetNewReleases(cs, &c)
	if err != nil {
		return err
	}

	if len(videos) == 0 {
		logging.I("No new releases for channel '%s'", c.URL)
	} else {
		if err := InitProcess(s.GetVideoStore(), c, videos); err != nil {
			return err
		}
	}

	// Update last scan time regardless of whether new videos were found
	if err := cs.UpdateLastScan(c.ID); err != nil {
		return fmt.Errorf("failed to update last scan time: %w", err)
	}

	notifyURLs, err := cs.GetNotifyURLs(c.ID)
	if err != nil {
		if err != sql.ErrNoRows {
			return err
		}
		logging.D(1, "No notification URL for channel with name '%s' and ID: %d", c.Name, c.ID)
	}

	if len(notifyURLs) > 0 {
		if errs := notify(&c, notifyURLs); len(errs) != 0 {
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
	return nil
}

// InitProcess begins the process for processing metadata/videos and respective downloads
func InitProcess(vs interfaces.VideoStore, c models.Channel, videos []*models.Video) error {
	var (
		wg      sync.WaitGroup
		errChan = make(chan error, len(videos))
	)

	logging.I("Starting meta/video processing for %d videos", len(videos))

	conc := c.Settings.Concurrency
	if conc < 1 {
		conc = 1
	}
	sem := make(chan struct{}, conc)

	for _, video := range videos {
		wg.Add(1)

		go func(v *models.Video) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			if err := processJSON(v, vs); err != nil {
				errChan <- fmt.Errorf("JSON processing error for %s: %w", v.URL, err)
				return
			}

			if logging.Level > 1 {
				fmt.Println()
				logging.I("Got requests for '%s'", v.URL)
				logging.P("Channel ID=%d", v.ChannelID)
				logging.P("Uploaded=%s", v.UploadDate)
			}

			if err := processVideo(v, vs); err != nil {
				errChan <- fmt.Errorf("video processing error for %s: %w", v.URL, err)
				return
			}

			// Check if Metarr exists on system (proceed if yes)
			if _, err := exec.LookPath("metarr"); err != nil {
				logging.I("Skipping Metarr process... 'metarr' not available: %v", err)
				return
			}
			InitMetarr(v)
		}(video)
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

// notify pings notification services as required
func notify(c *models.Channel, notifyURLs []string) (errs []error) {

	// Setup clients
	initClients()

	// Inner function
	notifyFunc := func(client *http.Client, notifyURL string) error {
		resp, err := client.Post(notifyURL, "application/json", nil)
		if err != nil {
			return fmt.Errorf("failed to send notification to URL '%s' for channel '%s' (ID: %d): %w",
				notifyURL, c.Name, c.ID, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			return fmt.Errorf("notification failed with status %d for channel '%s' (ID: %d)", resp.StatusCode, c.Name, c.ID)
		}
		return nil
	}

	// Notify for each URL
	for _, notifyURL := range notifyURLs {
		parsed, err := url.Parse(notifyURL)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid notification URL '%s': %v", notifyURL, err))
			continue
		}

		client := regClient
		if isPrivateNetwork(parsed.Host) {
			client = lanClient
		}

		if err := notifyFunc(client, notifyURL); err != nil {
			errs = append(errs, fmt.Errorf("failed to notify URL '%s': %v", notifyURL, err))
			continue
		}
		logging.S(1, "Successfully notified URL '%s' for channel '%s'", notifyURL, c.Name)
	}
	return errs
}
