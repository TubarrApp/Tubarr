// Package process begins processing the primary Tubarr program.
package process

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"tubarr/internal/downloads"
	"tubarr/internal/interfaces"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
)

var (
	muErr sync.Mutex
)

// InitProcess begins processing metadata/videos and respective downloads.
func InitProcess(s interfaces.Store, c *models.Channel, videos []*models.Video, ctx context.Context) (bool, []error) {
	var (
		errs    []error
		success bool
	)

	select {
	case <-ctx.Done():
		errs = append(errs, errors.New("aborting process, context canceled"))
		return false, errs
	default:
		// Process
	}
	conc := c.Settings.Concurrency
	if conc < 1 {
		conc = 1
	}

	logging.I("Starting meta/video processing for %d videos", len(videos))

	dlTracker := downloads.NewDownloadTracker(s.DownloadStore(), c.Settings.ExternalDownloader)
	dlTracker.Start(ctx)
	defer dlTracker.Stop()

	jobs := make(chan *models.Video, len(videos))
	results := make(chan error, len(videos))

	// Start workers
	for w := 1; w <= conc; w++ {
		go videoJob(w, jobs, results, s.VideoStore(), c, dlTracker, ctx)
	}

	// Send jobs
	for _, video := range videos {
		if video == nil {
			logging.E(0, "Video in queue for channel %q is nil", c.Name)
			continue
		}

		if video.Finished {
			logging.D(1, "Video in channel %q with URL %q is already marked as downloaded", c.Name, video.URL)
			continue
		}
		jobs <- video
	}
	close(jobs)

	for i := 0; i < len(videos); i++ {
		if err := <-results; err != nil {
			muErr.Lock()
			errs = append(errs, err)
			muErr.Unlock()
		} else {
			success = true
		}
	}

	if len(errs) > 0 {
		return success, errs
	}
	return success, nil
}

// videoJob starts a worker's process for a video.
func videoJob(id int, videos <-chan *models.Video, results chan<- error, vs interfaces.VideoStore, c *models.Channel, dlTracker *downloads.DownloadTracker, ctx context.Context) {
	for v := range videos {
		var err error

		// Initialize directory parser
		if strings.Contains(v.JSONDir, "{") || strings.Contains(v.VideoDir, "{") {
			dirParser := parsing.NewDirectoryParser(c, v)

			var parseDirs = []*string{
				&v.JSONDir, &v.VideoDir,
				&c.JSONDir, &c.VideoDir,
			}

			for _, ptr := range parseDirs {
				if ptr == nil {
					logging.E(0, "Null pointer in job with ID %d", id)
					continue
				}

				if *ptr, err = dirParser.ParseDirectory(*ptr); err != nil {
					logging.E(0, "Failed to parse directory %q", *ptr)
					continue
				}
			}
		}

		if v.JSONCustomFile == "" {
			proceed, err := processJSON(ctx, v, vs, dlTracker)
			if err != nil {
				results <- fmt.Errorf("JSON processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
				continue
			}

			if !proceed { // If the JSON filter failed, stop processing this video
				logging.I("Skipping further processing for ignored video: %s", v.URL)
				results <- nil
				continue
			}
		} else {
			_, err := vs.AddVideo(v)
			if err != nil {
				results <- fmt.Errorf("error adding video with URL %s to database: %w", v.URL, err)
			}
		}

		if logging.Level > 1 {
			fmt.Println()
			logging.I("Worker %d processing: %q", id, v.URL)
			logging.P("Channel ID=%d", v.ChannelID)
			logging.P("Uploaded=%s", v.UploadDate)
		}

		logging.I("About to process video with ID %d and URL %q", v.ID, v.URL)

		if err := processVideo(ctx, v, dlTracker); err != nil {
			results <- fmt.Errorf("video processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
			continue
		}

		// If Metarr is not available at all, mark as complete and return (user may not want to use it)
		if _, err := exec.LookPath("metarr"); err != nil {

			if errors.Is(err, exec.ErrNotFound) {
				logging.I("No Metarr $PATH, skipping Metarr process and marking video as finished")
				v.Finished = true
				if err := vs.UpdateVideo(v); err != nil {
					results <- fmt.Errorf("failed to update video DB entry: %w", err)
					continue
				}
				results <- nil
				continue
			}

			results <- fmt.Errorf("could not run Metarr at $PATH, check permissions and check that the file is executable (chmod +x)")
			continue
		}

		//
		if err := metarr.InitMetarr(v, ctx); err != nil {
			results <- fmt.Errorf("error initializing Metarr: %w", err)
			continue
		}

		v.Finished = true
		if err := vs.UpdateVideo(v); err != nil {
			results <- fmt.Errorf("failed to update video DB entry: %w", err)
		}
		results <- nil // nil = success
	}
}
