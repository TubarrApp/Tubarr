// Package process begins processing the primary Tubarr program.
package process

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"tubarr/internal/downloads"
	"tubarr/internal/interfaces"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
)

// InitProcess begins processing metadata/videos and respective downloads.
func InitProcess(s interfaces.Store, c *models.Channel, videos []*models.Video, ctx context.Context) (success bool, err error) {
	var (
		errs []error
	)

	select {
	case <-ctx.Done():
		errs = append(errs, errors.New("aborting process, context canceled"))
		return false, errors.Join(errs...)
	default:
		// Process
	}
	conc := max(c.Settings.Concurrency, 1)

	logging.I("Starting meta/video processing for %d videos", len(videos))

	dlTracker := downloads.NewDownloadTracker(s.DownloadStore(), c.Settings.ExternalDownloader)
	dlTracker.Start(ctx)
	defer dlTracker.Stop()

	jobs := make(chan *models.Video, len(videos))
	results := make(chan error, len(videos))

	dirParser := parsing.NewDirectoryParser(c)
	// Start workers ('w + 1' to start workers at 1)
	for w := range conc {
		go videoJob(w+1, jobs, results, s.VideoStore(), c, dlTracker, dirParser, ctx)
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

	for range videos {
		if err := <-results; err != nil {
			errs = append(errs, err)
		} else {
			success = true
		}
	}

	if len(errs) > 0 {
		logging.E(0, "Finished with %d errors", len(errs))
		return success, errors.Join(errs...)
	}
	return success, nil
}

// videoJob starts a worker's process for a video.
func videoJob(id int, videos <-chan *models.Video, results chan<- error, vs interfaces.VideoStore, c *models.Channel, dlTracker *downloads.DownloadTracker, dirParser *parsing.Directory, ctx context.Context) {
	for v := range videos {
		var err error

		// Initialize directory parser
		if strings.Contains(c.Settings.JSONDir, "{") || strings.Contains(c.Settings.VideoDir, "{") {

			if c.Settings.JSONDir, err = dirParser.ParseDirectory(c.Settings.JSONDir, v, "JSON"); err != nil {
				logging.E(0, "Failed to parse JSON directory %q", c.Settings.JSONDir)
			}

			if c.Settings.VideoDir, err = dirParser.ParseDirectory(c.Settings.VideoDir, v, "video"); err != nil {
				logging.E(0, "Failed to parse video directory %q", c.Settings.VideoDir)
			}
		}

		if v.JSONCustomFile == "" {
			proceed, err := processJSON(ctx, v, vs, dirParser, dlTracker)
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

		// Initialize Metarr
		if err := metarr.InitMetarr(v, dirParser, ctx); err != nil {
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
