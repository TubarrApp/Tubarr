// Package app contains core application functionality.
package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
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
	"tubarr/internal/utils/times"
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
	_, metarrErr := exec.LookPath("metarr")
	metarrExists := metarrErr == nil
	if metarrErr != nil && !errors.Is(metarrErr, exec.ErrNotFound) {
		return 0, 0, fmt.Errorf("unexpected error checking for 'metarr': %w", metarrErr)
	}

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
		video.ParsedMetaDir, video.ParsedVideoDir = parseVideoJSONDirs(video, cu, c, dirParser)

		// Use custom scraper if needed
		if video.JSONPath, err = executeCustomScraper(scrape, video); err != nil {
			logging.E("Custom scraper failed for %q: %v", video.URL, err)
			errs = append(errs, err)
			continue
		}

		// Skip already downloaded videos
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
			logging.E("Video processing failed: %v", err)
			errs = append(errs, err)
		} else {
			nSucceeded++
		}
	}

	// Count videos that were actually downloaded (not skipped)
	for _, video := range videos {
		if video.Finished && !video.Ignored {
			nDownloaded++
		}
	}

	if len(errs) > 0 {
		logging.E("Finished with %d error(s): %v", len(errs), err)
		return nSucceeded, nDownloaded, errors.Join(errs...)
	}
	return nSucceeded, nDownloaded, nil
}

// executeCustomScraper checks if a custom scraper should be used for this release.
func executeCustomScraper(s *scraper.Scraper, v *models.Video) (customJSON string, err error) {
	if v == nil || s == nil {
		return "", fmt.Errorf("invalid nil parameter (video: %v, scraper: %v)", v == nil, s == nil)
	}

	// Detect censored.tv links
	if strings.Contains(v.URL, "censored.tv") {
		if !dev.CensoredTVUseCustom {
			logging.I("Using regular scraper for censored.tv ...")
		} else {
			logging.I("Detected a censored.tv link. Using specialized scraper.")
			err := s.ScrapeCensoredTVMetadata(v.URL, v.ParsedMetaDir, v)
			if err != nil {
				return "", fmt.Errorf("failed to scrape censored.tv metadata: %w", err)
			}
		}
	}
	return v.JSONCustomFile, nil
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
	if err := times.WaitTime(procCtx, times.RandomSecsDuration(consts.DefaultBotAvoidanceDelay), c.Name, v.URL); err != nil {
		return err
	}

	proceed, botBlockChannel, err := processJSON(procCtx, vs, dlTracker, dirParser, c, cu, v)
	if err != nil {
		handleBotBlock(cs, c, cu, botBlockChannel)
		return fmt.Errorf("metadata processing error for video URL %q: %w", v.URL, err)
	}
	if !proceed {
		logging.I("Skipping further processing for ignored video: %s", v.URL)
		return nil
	}

	// Process video download phase
	botBlockChannel, err = processVideo(procCtx, v, cu, c, dlTracker)
	if err != nil {
		handleBotBlock(cs, c, cu, botBlockChannel)
		return fmt.Errorf("video processing error for video URL %q: %w", v.URL, err)
	}

	// Check if Metarr is available
	if !metarrExists {
		logging.I("No 'metarr' at $PATH, skipping Metarr process and marking video as finished")
		return completeAndStoreVideo(vs, v, c)
	}

	// Run metarr step
	if err := metarr.InitMetarr(procCtx, v, cu, c, dirParser); err != nil {
		return fmt.Errorf("metarr processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
	}

	return completeAndStoreVideo(vs, v, c)
}

// completeAndStoreVideo marks a video as complete.
func completeAndStoreVideo(vs contracts.VideoStore, v *models.Video, c *models.Channel) error {
	v.MarkVideoAsCompleted()
	if err := vs.UpdateVideo(v, c.ID); err != nil {
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
	if v == nil || cu == nil || c == nil {
		return false, false, fmt.Errorf("invalid nil parameter (video: %v, channelURL: %v, channel: %v)", v == nil, cu == nil, c == nil)
	}

	// ONLY download JSON if it's NOT a custom file (custom scraper already created it)
	if v.JSONCustomFile == "" {
		dl, err := downloads.NewJSONDownload(procCtx, v, cu, c, dlTracker, &downloads.Options{
			MaxRetries:       3,
			RetryMaxInterval: 5,
		})
		if err != nil {
			return false, false, err
		}

		if botBlockChannel, err = dl.Execute(); err != nil {
			return false, botBlockChannel, err
		}
	} else {
		logging.D(1, "Skipping JSON download - using custom scraped file: %s", v.JSONCustomFile)
	}

	// Validate and filter (delegates to metadata package)
	passedChecks, useFilteredMetaOps, useFilteredFilenameOps, err := metadata.ValidateAndFilter(v, cu, c, dirParser)
	if err != nil {
		return false, false, err
	}
	// Store filtered operations in the video (per-video, not shared)
	v.FilteredMetaOps = useFilteredMetaOps
	v.FilteredFilenameOps = useFilteredFilenameOps

	if !passedChecks {
		v.MarkVideoAsIgnored()
		if v.ID, err = vs.AddVideo(v, c.ID, cu.ID); err != nil {
			return false, false, fmt.Errorf("failed to update ignored video: %w", err)
		}
		return false, false, nil
	}

	// Will download this video (passed checks)
	v.DownloadStatus.Status = consts.DLStatusPending
	v.DownloadStatus.Pct = 0.0
	logging.D(1, "Setting video %q to Pending status before saving to DB", v.URL)

	// Save video to database
	if v.ID, err = vs.AddVideo(v, c.ID, cu.ID); err != nil {
		return false, false, fmt.Errorf("failed to update video DB entry: %w", err)
	}
	logging.D(1, "Saved video %q (ID: %d) to DB with Pending status", v.URL, v.ID)

	logging.S("Processed metadata for: %s", v.URL)
	return true, false, nil
}

// processVideo processes video downloads.
func processVideo(procCtx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, dlTracker *downloads.DownloadTracker) (botBlockChannel bool, err error) {
	if v == nil || cu == nil || c == nil {
		return false, fmt.Errorf("invalid nil parameter (video: %v, channelURL: %v, channel: %v)", v == nil, cu == nil, c == nil)
	}

	logging.I("Processing video download for URL: %s", v.URL)

	// Create cancellable context for this specific download
	downloadCtx, cancel := context.WithCancel(procCtx)
	defer cancel()

	// Register the cancel function so it can be called from API
	downloads.RegisterDownloadContext(v.ID, v.URL, cancel)
	defer downloads.UnregisterDownloadContext(v.ID, v.URL)

	dl, err := downloads.NewVideoDownload(downloadCtx, v, cu, c, dlTracker, &downloads.Options{
		MaxRetries:       3,
		RetryMaxInterval: 20,
	})
	if err != nil {
		return false, err
	}

	if botBlockChannel, err = dl.Execute(); err != nil {
		return botBlockChannel, err
	}

	logging.D(1, "Successfully processed and marked as downloaded: %s", v.URL)
	return false, nil
}

// parseVideoJSONDirs parses video and JSON directories.
func parseVideoJSONDirs(v *models.Video, cu *models.ChannelURL, c *models.Channel, dirParser *parsing.DirectoryParser) (metaDir, videoDir string) {
	// Initialize dirParser if nil
	if dirParser == nil {
		dirParser = parsing.NewDirectoryParser(c)
	}

	var (
		cSettings = cu.ChanURLSettings
		err       error
	)

	// Return if no template directives
	if !strings.Contains(cSettings.JSONDir, "{") && !strings.Contains(cSettings.VideoDir, "{") {
		return cSettings.JSONDir, cSettings.VideoDir
	}

	// Parse templates
	metaDir, err = dirParser.ParseDirectory(cSettings.JSONDir, v, "JSON")
	if err != nil {
		logging.W("Failed to parse JSON directory %q, using raw: %v", cSettings.JSONDir, err)
		metaDir = cSettings.JSONDir
	}

	videoDir, err = dirParser.ParseDirectory(cSettings.VideoDir, v, "video")
	if err != nil {
		logging.W("Failed to parse video directory %q, using raw: %v", cSettings.VideoDir, err)
		videoDir = cSettings.VideoDir
	}

	return metaDir, videoDir
}

// handleBotBlock handles cases where the program has been detected as a bot.
func handleBotBlock(cs contracts.ChannelStore, c *models.Channel, cu *models.ChannelURL, botBlock bool) {
	if !botBlock {
		return
	}
	if err := blockChannelBotDetected(cs, c, cu); err != nil {
		logging.E("Failed to block channel: %v", err)
	}
}
