// Package process begins processing the primary Tubarr program.
package process

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	"tubarr/internal/downloads"
	"tubarr/internal/interfaces"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
)

// InitProcess begins processing metadata/videos and respective downloads.
func InitProcess(s interfaces.Store, c *models.Channel, videos []*models.Video, ctx context.Context) (nSucceeded int, err error) {
	var (
		errs []error
	)

	conc := max(c.Settings.Concurrency, 1)

	logging.I("Starting meta/video processing for %d videos", len(videos))

	dlTracker := downloads.NewDownloadTracker(s.DownloadStore(), c.Settings.ExternalDownloader)
	dlTracker.Start(ctx)
	defer dlTracker.Stop()

	jobs := make(chan *models.Video, len(videos))
	results := make(chan error, len(videos))

	dirParser := parsing.NewDirectoryParser(c)

	// Check if Metarr exists
	_, err = exec.LookPath("metarr")
	if err != nil {
		if !errors.Is(err, exec.ErrNotFound) {
			return 0, fmt.Errorf("could not find 'metarr' at $PATH, check permissions and ensure the file is executable")
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
				err := videoJob(ctx, v, s.VideoStore(), dirParser, dlTracker, metarrExists)
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

	if len(errs) > 0 {
		logging.E(0, "Finished with %d errors", len(errs))
		return nSucceeded, errors.Join(errs...)
	}
	return nSucceeded, nil
}

// videoJob starts a worker's process for a video.
func videoJob(
	ctx context.Context,
	v *models.Video,
	vs interfaces.VideoStore,
	dirParser *parsing.Directory,
	dlTracker *downloads.DownloadTracker,
	metarrExists bool,
) error {
	if v.JSONCustomFile == "" {
		proceed, err := processJSON(ctx, v, vs, dirParser, dlTracker)
		if err != nil {
			return fmt.Errorf("json processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
		}
		if !proceed {
			logging.I("Skipping further processing for ignored video: %s", v.URL)
			return nil
		}
	} else {
		if _, err := vs.AddVideo(v); err != nil {
			return fmt.Errorf("error adding video with URL %s to database: %w", v.URL, err)
		}
	}

	if err := processVideo(ctx, v, dlTracker); err != nil {
		return fmt.Errorf("video processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
	}

	if !metarrExists {
		logging.I("No 'metarr' at $PATH, skipping Metarr process and marking video as finished")
		v.Finished = true
		if err := vs.UpdateVideo(v); err != nil {
			return fmt.Errorf("failed to update video DB entry: %w", err)
		}
		return nil // stop here, don't continue with metarr
	}

	// Run metarr step
	if err := metarr.InitMetarr(v, dirParser, ctx); err != nil {
		return fmt.Errorf("metarr processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
	}

	return nil
}
