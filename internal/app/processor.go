// Package app contains core application functionality.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"tubarr/internal/abstractions"
	"tubarr/internal/contracts"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/keys"
	"tubarr/internal/domain/logger"
	"tubarr/internal/domain/vars"
	"tubarr/internal/downloads"
	"tubarr/internal/file"
	"tubarr/internal/metadata"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/scraper"
	"tubarr/internal/times"

	"github.com/TubarrApp/gocommon/sharedvalidation"
)

// InitProcess begins processing metadata/videos and respective downloads.
func InitProcess(
	ctx context.Context,
	s contracts.Store,
	c *models.Channel,
	cu *models.ChannelURL,
	videos []*models.Video,
	scrape *scraper.Scraper) (nSucceeded int, nDownloaded int, err error) {
	// Initialize variables.
	var (
		errs []error
		conc = sharedvalidation.ValidateConcurrencyLimit(c.ChanSettings.Concurrency)
	)

	logger.Pl.I("Starting meta/video processing for %d videos", len(videos))

	// Bot detection context.
	procCtx, procCancel := context.WithCancel(ctx)
	defer procCancel()

	dlTracker := downloads.NewDownloadTracker(s.DownloadStore(), c.ChanSettings.ExternalDownloader)
	dlTracker.Start(ctx)
	defer dlTracker.Stop()

	jobs := make(chan *models.Video, len(videos))
	results := make(chan error, len(videos))

	// Parse directories.
	dirParser := parsing.NewDirectoryParser(c, parsing.TubarrTags)
	if c.ChanSettings.VideoDir, err = dirParser.ParseDirectory(c.ChanSettings.VideoDir, "channel video directory"); err != nil {
		return 0, 0, err
	}
	if c.ChanSettings.JSONDir, err = dirParser.ParseDirectory(c.ChanSettings.JSONDir, "channel JSON directory"); err != nil {
		return 0, 0, err
	}
	if cu.ChanURLSettings.VideoDir, err = dirParser.ParseDirectory(cu.ChanURLSettings.VideoDir, "channel URL video directory"); err != nil {
		return 0, 0, err
	}
	if cu.ChanURLSettings.JSONDir, err = dirParser.ParseDirectory(cu.ChanURLSettings.JSONDir, "channel URL JSON directory"); err != nil {
		return 0, 0, err
	}

	// Check if Metarr exists and can be run.
	_, metarrErr := exec.LookPath("metarr")
	if metarrErr == nil { // If err IS nil.
		if err := exec.CommandContext(ctx, "metarr", "--help").Run(); err != nil { // Check path can be executed (not just exists).
			metarrErr = fmt.Errorf("found 'metarr' in $PATH but failed to execute: %w", err)
		}
	}

	metarrIsExecutable := metarrErr == nil
	if metarrErr != nil {
		logger.Pl.E("Cannot run 'metarr' due to error: %v", metarrErr)
	}

	// Start workers.
	var wg sync.WaitGroup
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
					metarrIsExecutable)

				// If bot was detected, cancel the context to stop other workers.
				if err != nil && strings.Contains(err.Error(), consts.BotActivitySentinel) {
					procCancel()
				}
				results <- err
			}
		})
	}

	// Send jobs.
	for _, video := range videos {
		if video == nil {
			logger.Pl.E("Video in queue for channel %q is nil", c.Name)
			continue
		}

		// Get video paths.
		video.JSONDir = cu.ChanURLSettings.JSONDir
		video.VideoDir = cu.ChanURLSettings.VideoDir

		// Use custom scraper if needed.
		if video.JSONFilePath, err = executeCustomScraper(scrape, video, c); err != nil {
			logger.Pl.E("Custom scraper failed for %q: %v", video.URL, err)
			errs = append(errs, err)
			continue
		}

		// Skip already downloaded videos.
		if video.Finished {
			logger.Pl.D(1, "Video in channel %q with URL %q is already marked as downloaded", c.Name, video.URL)
			continue
		}
		jobs <- video
	}
	close(jobs)

	// Close results after all workers are done.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results.
	for err := range results {
		if err != nil {
			logger.Pl.E("Video processing failed: %v", err)
			errs = append(errs, err)
		} else {
			nSucceeded++
		}
	}

	// Count videos that were actually downloaded (not skipped).
	for _, video := range videos {
		if video.Finished && !video.Ignored {
			nDownloaded++
		}
	}

	if len(errs) > 0 {
		logger.Pl.E("Finished with %d error(s): %v", len(errs), err)
		return nSucceeded, nDownloaded, errors.Join(errs...)
	}
	return nSucceeded, nDownloaded, nil
}

// executeCustomScraper checks if a custom scraper should be used for this release.
func executeCustomScraper(s *scraper.Scraper, v *models.Video, c *models.Channel) (customJSON string, err error) {
	if v == nil || s == nil {
		return "", fmt.Errorf("invalid nil parameter (video: %v, scraper: %v)", v == nil, s == nil)
	}

	// Detect custom site.
	s.ScrapeCustomSite(v.URL, v.JSONFilePath, v, c)

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
	metarrIsExecutable bool,
) (err error) {
	// Bot detection avoidance wait time.
	if err := times.WaitTime(procCtx, times.RandomSecsDuration(consts.DefaultBotAvoidanceSeconds), c.Name, v.URL); err != nil {
		return err
	}

	// Process JSON metadata phase.
	proceed, botBlockChannel, err := processJSON(procCtx, vs, dlTracker, dirParser, c, cu, v)
	if err != nil {
		handleBotBlock(cs, c, cu, botBlockChannel)
		return fmt.Errorf("metadata processing error for video URL %q: %w", v.URL, err)
	}
	if !proceed {
		logger.Pl.I("Skipping further processing for ignored video: %s", v.URL)
		return nil
	}

	// Process video download phase.
	botBlockChannel, err = processVideo(procCtx, v, cu, c, dlTracker)
	if err != nil {
		handleBotBlock(cs, c, cu, botBlockChannel)
		return fmt.Errorf("video processing error for video URL %q: %w", v.URL, err)
	}

	// Check if Metarr can be run.
	if !metarrIsExecutable {
		logger.Pl.I("No 'metarr' at $PATH, skipping Metarr process and marking video as finished")
		return completeAndStoreVideo(cs, vs, v, c)
	}

	// Run metarr step.
	if err := metarr.InitMetarr(procCtx, v, cu, c, dirParser); err != nil {
		return fmt.Errorf("metarr processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
	}

	return completeAndStoreVideo(cs, vs, v, c)
}

// completeAndStoreVideo marks a video as complete.
func completeAndStoreVideo(cs contracts.ChannelStore, vs contracts.VideoStore, v *models.Video, c *models.Channel) error {
	v.MarkVideoAsComplete()

	// Handle metadata file purging if configured.
	if abstractions.IsSet(keys.PurgeMetaFile) {
		if abstractions.GetBool(keys.PurgeMetaFile) {
			logger.Pl.I("Purging metadata file for video with URL %q as per configuration", v.URL)
			if err := os.Remove(v.JSONFilePath); err != nil && !os.IsNotExist(err) {
				logger.Pl.W("Failed to delete metadata file %q: %v", v.JSONFilePath, err)
			} else {
				logger.Pl.D(1, "Deleted metadata file %q successfully", v.JSONFilePath)
				v.JSONFilePath = ""
			}
		}
	}

	// Update video in database.
	if err := vs.UpdateVideo(v, c.ID); err != nil {
		return fmt.Errorf("failed to update video DB entry: %w", err)
	}

	// Update channel's last video added timestamp.
	cIDStr := strconv.FormatInt(c.ID, 10)

	// Lock for array update.
	vars.UpdateNewVideoURLMutex.Lock()
	defer vars.UpdateNewVideoURLMutex.Unlock()

	// Get and store new video URLs.
	newVideoURLs, err := cs.GetNewVideoURLs(consts.QChanID, cIDStr)
	if err != nil {
		logger.Pl.E("Could not fetch new video URLs: %v", err)
	} else {
		newVideoURLs = append(newVideoURLs, v.URL)

		// Update new video URLs into the database.
		if err := cs.UpdateNewVideoURLs(consts.QChanID, cIDStr, newVideoURLs); err != nil {
			logger.Pl.E("Could not store new video URLs: %v", err)
		}
	}

	// Set new video notification to true.
	c.NewVideoNotification = true
	if err := cs.UpdateChannelValue(consts.QChanID, fmt.Sprintf("%d", c.ID), consts.QChanNewVideoNotification, c.NewVideoNotification); err != nil {
		logger.Pl.E("Failed to update channel %q notification status: %v", c.Name, err)
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

	// ONLY download JSON if it's NOT a custom file (custom scraper already created it).
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
		logger.Pl.D(1, "Skipping JSON download - using custom scraped file: %s", v.JSONCustomFile)
	}

	// Validate and filter.
	passedChecks, useFilteredMetaOps, useFilteredFilenameOps, err := metadata.ValidateAndFilter(v, cu, c, dirParser)
	if err != nil {
		return false, false, err
	}
	// Store filtered operations in the video (per-video, not shared).
	v.FilteredMetaOps = useFilteredMetaOps
	v.FilteredFilenameOps = useFilteredFilenameOps

	// If video failed checks, mark and save to DB.
	if !passedChecks {
		// Remove metadata JSON file since video is being ignored.
		if v.JSONFilePath != "" {
			if err := file.RemoveMetadataJSON(v.JSONFilePath); err != nil && !os.IsNotExist(err) {
				logger.Pl.W("Failed to remove metadata file for ignored video %q: %v", v.JSONFilePath, err)
			}
		}

		v.MarkVideoAsIgnored()
		if v.ID, err = vs.AddVideo(v, c.ID, cu.ID); err != nil {
			return false, false, fmt.Errorf("failed to update ignored video: %w", err)
		}
		return false, false, nil
	}

	// Will download this video (passed checks).
	v.DownloadStatus.Status = consts.DLStatusQueued
	v.DownloadStatus.Percent = 0.0
	logger.Pl.D(1, "Setting video %q to Queued status before saving to DB", v.URL)

	// Save video to database.
	if v.ID, err = vs.AddVideo(v, c.ID, cu.ID); err != nil {
		return false, false, fmt.Errorf("failed to update video DB entry: %w", err)
	}

	logger.Pl.S("Processed metadata for: %s", v.URL)
	return true, false, nil
}

// processVideo processes video downloads.
func processVideo(procCtx context.Context, v *models.Video, cu *models.ChannelURL, c *models.Channel, dlTracker *downloads.DownloadTracker) (botBlockChannel bool, err error) {
	if v == nil || cu == nil || c == nil {
		return false, fmt.Errorf("invalid nil parameter (video: %v, channelURL: %v, channel: %v)", v == nil, cu == nil, c == nil)
	}

	logger.Pl.I("Processing video download for URL: %s", v.URL)

	// Create cancellable context for this specific download.
	downloadCtx, cancel := context.WithCancel(procCtx)
	defer cancel()

	// Register the cancel function so it can be called from API.
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

	logger.Pl.D(1, "Successfully processed and marked as downloaded: %s", v.URL)
	return false, nil
}

// handleBotBlock handles cases where the program has been detected as a bot.
func handleBotBlock(cs contracts.ChannelStore, c *models.Channel, cu *models.ChannelURL, botBlock bool) {
	if !botBlock {
		return
	}
	if err := blockChannelBotDetected(cs, c, cu); err != nil {
		logger.Pl.E("Failed to block channel: %v", err)
	}
}
