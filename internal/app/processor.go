// Package app contains core application functionality.
package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
	"tubarr/internal/contracts"
	"tubarr/internal/dev"
	"tubarr/internal/domain/consts"
	"tubarr/internal/downloads"
	"tubarr/internal/metadata"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/scraper"
	"tubarr/internal/utils/logging"
)

// InitProcess begins processing metadata/videos and respective downloads.
func InitProcess(
	ctx context.Context,
	s contracts.Store,
	c *models.Channel,
	cu *models.ChannelURL,
	videos []*models.Video,
	scrape *scraper.Scraper) (nSucceeded int, nDownloaded int, err error) {
	// Initialize variables
	var (
		errs []error
		conc = max(c.ChanSettings.Concurrency, 1)
	)

	logging.I("Starting meta/video processing for %d videos", len(videos))

	// Bot detection context
	procCtx, procCancel := context.WithCancel(ctx)
	defer procCancel()

	dlTracker := downloads.NewDownloadTracker(s.DownloadStore(), c.ChanSettings.ExternalDownloader)
	dlTracker.Start(ctx)
	defer dlTracker.Stop()

	jobs := make(chan *models.Video, len(videos))
	results := make(chan error, len(videos))

	dirParser := parsing.NewDirectoryParser(c)

	// Check if Metarr exists
	_, err = exec.LookPath("metarr")
	if err != nil {
		if !errors.Is(err, exec.ErrNotFound) {
			return 0, 0, fmt.Errorf("error checking for 'metarr' at $PATH: %w", err)
		}
	}
	metarrExists := (err == nil)

	var wg sync.WaitGroup

	// Start workers
	for range conc {
		wg.Go(func() {
			for v := range jobs {
				select {
				case <-procCtx.Done():
					results <- fmt.Errorf("skipped %q: %w", v.URL, procCtx.Err())
					continue
				default:
				}

				err := videoJob(
					procCtx,
					s.VideoStore(),
					s.ChannelStore(),
					dlTracker,
					dirParser,
					v,
					cu,
					c,
					metarrExists)

				// If bot was detected, cancel the context to stop other workers
				if err != nil && strings.Contains(err.Error(), consts.BotActivitySentinel) {
					procCancel()
				}
				results <- err
			}
		})
	}

	// Send jobs
	for _, video := range videos {
		if video == nil {
			logging.E("Video in queue for channel %q is nil", c.Name)
			continue
		}

		// Parse directories (templating options can include video elements)
		video.ParsedJSONDir, video.ParsedVideoDir = parseVideoJSONDirs(video, dirParser)

		// Check for custom scraper needs
		if err := checkCustomScraperNeeds(scrape, video); err != nil {
			return nSucceeded, nDownloaded, err
		}

		if video.Finished {
			logging.D(1, "Video in channel %q with URL %q is already marked as downloaded", c.Name, video.URL)
			continue
		}
		jobs <- video
	}
	close(jobs)

	// Close results after all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	for err := range results {
		if err != nil {
			errs = append(errs, err)
		} else {
			nSucceeded++
		}
	}

	// Count videos that were actually downloaded (not skipped)
	for _, video := range videos {
		if video.Finished && !video.WasSkipped {
			nDownloaded++
		}
	}

	if len(errs) > 0 {
		logging.E("Finished with %d errors", len(errs))
		return nSucceeded, nDownloaded, errors.Join(errs...)
	}
	return nSucceeded, nDownloaded, nil
}

// checkCustomScraperNeeds checks if a custom scraper should be used for this release.
func checkCustomScraperNeeds(s *scraper.Scraper, v *models.Video) error {
	// Detect censored.tv links
	if strings.Contains(v.URL, "censored.tv") {
		if !dev.CensoredTVUseCustom {
			logging.I("Using regular scraper for censored.tv ...")
		} else {
			logging.I("Detected a censored.tv link. Using specialized scraper.")
			err := s.ScrapeCensoredTVMetadata(v.URL, v.ParsedJSONDir, v)
			if err != nil {
				return fmt.Errorf("failed to scrape censored.tv metadata: %w", err)
			}
		}
	}
	return nil
}

// videoJob starts a worker's process for a video.
func videoJob(
	procCtx context.Context,
	vs contracts.VideoStore,
	cs contracts.ChannelStore,
	dlTracker *downloads.DownloadTracker,
	dirParser *parsing.DirectoryParser,
	v *models.Video,
	cu *models.ChannelURL,
	c *models.Channel,
	metarrExists bool,
) error {
	// Process JSON phase
	if v.JSONCustomFile == "" {
		proceed, err := handleJSONProcessing(procCtx, cs, vs, dlTracker, dirParser, c, cu, v)
		if err != nil {
			return err
		}
		if !proceed {
			logging.I("Skipping further processing for ignored video: %s", v.URL)
			return nil
		}
	} else {
		if _, err := vs.AddVideo(v, c); err != nil {
			return fmt.Errorf("error adding video with URL %s to database: %w", v.URL, err)
		}
	}

	// Process video download phase
	botBlockChannel, err := processVideo(procCtx, v, cu, c, dlTracker)
	if err != nil {
		return handleBotError(cs, c, cu, v.URL, botBlockChannel, err, "video processing")
	}

	// Check if Metarr is available
	if !metarrExists {
		logging.I("No 'metarr' at $PATH, skipping Metarr process and marking video as finished")
		return markVideoComplete(vs, v, c)
	}

	// Run metarr step
	if err := metarr.InitMetarr(procCtx, v, cu, c, dirParser); err != nil {
		return fmt.Errorf("metarr processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
	}

	return markVideoComplete(vs, v, c)
}

// handleJSONProcessing processes JSON metadata with bot detection and error handling.
func handleJSONProcessing(
	procCtx context.Context,
	cs contracts.ChannelStore,
	vs contracts.VideoStore,
	dlTracker *downloads.DownloadTracker,
	dirParser *parsing.DirectoryParser,
	c *models.Channel,
	cu *models.ChannelURL,
	v *models.Video,
) (bool, error) {
	// Process JSON downloading and filtering for this file.
	proceed, botBlockChannel, err := processJSON(procCtx, vs, dlTracker, dirParser, c, cu, v)
	if err != nil {
		return false, handleBotError(cs, c, cu, v.URL, botBlockChannel, err, "JSON processing")
	}

	return proceed, nil
}

// handleBotError handles bot detection errors with channel blocking.
func handleBotError(
	cs contracts.ChannelStore,
	c *models.Channel,
	cu *models.ChannelURL,
	videoURL string,
	botBlockChannel bool,
	err error,
	phase string,
) error {
	// Check if the channel is blocked due to bot activity
	if botBlockChannel {
		if blockErr := blockChannelBotDetected(cs, c, cu); blockErr != nil {
			logging.E("Failed to block channel: %v", blockErr)
		}
	}
	return fmt.Errorf("%s error for video URL %q: %w", phase, videoURL, err)
}

// markVideoComplete marks a video as complete.
func markVideoComplete(vs contracts.VideoStore, v *models.Video, c *models.Channel) error {
	v.Finished = true
	if err := vs.UpdateVideo(v, c); err != nil {
		return fmt.Errorf("failed to update video DB entry: %w", err)
	}
	return nil
}

// processJSON coordinates JSON download, validation, and database updates.
func processJSON(
	procCtx context.Context,
	vs contracts.VideoStore,
	dlTracker *downloads.DownloadTracker,
	dirParser *parsing.DirectoryParser,
	c *models.Channel,
	cu *models.ChannelURL,
	v *models.Video,
) (proceed, botBlockChannel bool, err error) {
	// Download JSON
	dl, err := downloads.NewJSONDownload(procCtx, v, cu, c, dlTracker, &downloads.Options{
		MaxRetries:    3,
		RetryInterval: 5 * time.Second,
	})
	if err != nil {
		return false, false, err
	}

	if botBlockChannel, err := dl.Execute(); err != nil {
		return false, botBlockChannel, err
	}

	// Validate and filter (delegates to metadata package)
	passedChecks, err := metadata.ValidateAndFilter(v, cu, c, dirParser)
	if err != nil {
		return false, false, err
	}

	if !passedChecks {
		// Mark as skipped
		v.DownloadStatus.Status = consts.DLStatusCompleted
		v.DownloadStatus.Pct = 100.0
		v.Finished = true
		v.WasSkipped = true

		if v.ID, err = vs.AddVideo(v, c); err != nil {
			return false, false, fmt.Errorf("failed to update ignored video: %w", err)
		}
		return false, false, nil
	}

	// Save video to database
	if v.ID, err = vs.AddVideo(v, c); err != nil {
		return false, false, fmt.Errorf("failed to update video DB entry: %w", err)
	}

	logging.S(0, "Processed metadata for: %s", v.URL)
	return true, false, nil
}

// processVideo processes video downloads.
func processVideo(procCtx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, dlTracker *downloads.DownloadTracker) (botBlockChannel bool, err error) {
	if v == nil {
		logging.I("Null video entered")
		return false, nil
	}

	logging.I("Processing video download for URL: %s", v.URL)

	dl, err := downloads.NewVideoDownload(procCtx, v, cu, c, dlTracker, &downloads.Options{
		MaxRetries:    3,
		RetryInterval: 5 * time.Second,
	})
	if err != nil {
		return false, err
	}

	if botBlockChannel, err := dl.Execute(); err != nil {
		if botBlockChannel {
			return true, err // Only 'TRUE' bot pause channel path
		}
		return false, err
	}

	logging.D(1, "Successfully processed and marked as downloaded: %s", v.URL)
	return false, nil
}

// parseVideoJSONDirs parses video and JSON directories.
func parseVideoJSONDirs(v *models.Video, dirParser *parsing.DirectoryParser) (jsonDir, videoDir string) {
	// Initialize directory parser
	var (
		cSettings = v.Settings
		err       error
	)

	if strings.Contains(cSettings.JSONDir, "{") || strings.Contains(cSettings.VideoDir, "{") {

		jsonDir, err = dirParser.ParseDirectory(cSettings.JSONDir, v, "JSON")
		if err != nil {
			logging.E("Failed to parse JSON directory %q", cSettings.JSONDir)
		}
		videoDir, err = dirParser.ParseDirectory(cSettings.VideoDir, v, "video")
		if err != nil {
			logging.E("Failed to parse video directory %q", cSettings.VideoDir)
		}
	}

	return jsonDir, videoDir
}
