// Package process begins processing the primary Tubarr program.
package process

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"tubarr/internal/domain/consts"
	"tubarr/internal/downloads"
	"tubarr/internal/interfaces"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
)

// InitProcess begins processing metadata/videos and respective downloads.
func InitProcess(ctx context.Context, s interfaces.Store, cu *models.ChannelURL, c *models.Channel, videos []*models.Video) (nSucceeded int, nDownloaded int, err error) {
	var (
		errs []error
	)

	conc := max(c.ChanSettings.Concurrency, 1)

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
	metarrExists := err == nil

	var wg sync.WaitGroup

	// Start workers
	for w := range conc {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for v := range jobs {
				// Check if context was cancelled (bot detected by another worker)
				select {
				case <-procCtx.Done():
					results <- fmt.Errorf("skipped %q: %w", v.URL, procCtx.Err())
					continue
				default:
				}

				err := videoJob(
					procCtx,
					s.ChannelStore(),
					s.VideoStore(),
					v,
					cu,
					c,
					dirParser,
					dlTracker,
					metarrExists)

				// If bot was detected, cancel the context to stop other workers
				if err != nil && strings.Contains(err.Error(), consts.BotActivitySentinel) {
					procCancel()
				}

				results <- err
			}
		}(w + 1)
	}

	// Send jobs
	for _, video := range videos {
		if video == nil {
			logging.E(0, "Video in queue for channel %q is nil", c.Name)
			continue
		}

		// Parse directories (templating options can include video elements)
		video.ParsedJSONDir, video.ParsedVideoDir = parseVideoJSONDirs(video, dirParser)

		// Check for custom scraper needs
		if err := checkCustomScraperNeeds(video); err != nil {
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
		logging.E(0, "Finished with %d errors", len(errs))
		return nSucceeded, nDownloaded, errors.Join(errs...)
	}
	return nSucceeded, nDownloaded, nil
}

// videoJob starts a worker's process for a video.
func videoJob(
	procCtx context.Context,
	cs interfaces.ChannelStore,
	vs interfaces.VideoStore,
	v *models.Video,
	cu *models.ChannelURL,
	c *models.Channel,
	dirParser *parsing.Directory,
	dlTracker *downloads.DownloadTracker,
	metarrExists bool,
) error {

	if v.JSONCustomFile == "" {
		proceed, botPauseChannel, err := processJSON(procCtx, v, cu, c, vs, dirParser, dlTracker)
		if err != nil {
			if botPauseChannel {
				if blockErr := blockChannelBotDetected(cs, c, cu); blockErr != nil {
					logging.E(0, "Failed to block channel: %v", blockErr)
				}
			}
			return fmt.Errorf("json processing error for video URL %q: %w", v.URL, err)
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

	botPauseChannel, err := processVideo(procCtx, v, cu, c, dlTracker)
	if err != nil {
		if botPauseChannel {
			if blockErr := blockChannelBotDetected(cs, c, cu); blockErr != nil {
				logging.E(0, "Failed to block channel: %v", blockErr)
			}
		}
		return fmt.Errorf("json processing error for video URL %q: %w", v.URL, err)
	}

	if !metarrExists {
		logging.I("No 'metarr' at $PATH, skipping Metarr process and marking video as finished")
		return markVideoComplete(vs, v, c)
	}

	// Run metarr step
	if err := metarr.InitMetarr(v, cu, c, dirParser, procCtx); err != nil {
		return fmt.Errorf("metarr processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
	}

	return markVideoComplete(vs, v, c)
}

// completeVideo marks a video as complete.
func markVideoComplete(vs interfaces.VideoStore, v *models.Video, c *models.Channel) error {
	v.Finished = true
	if err := vs.UpdateVideo(v, c); err != nil {
		return fmt.Errorf("failed to update video DB entry: %w", err)
	}
	return nil
}
