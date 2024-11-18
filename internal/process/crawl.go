package process

import (
	"fmt"
	"os/exec"
	"sync"
	"time"
	"tubarr/internal/cfg"
	"tubarr/internal/domain/keys"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/utils/browser"
	"tubarr/internal/utils/logging"
)

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
