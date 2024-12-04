// Package process begins processing the primary Tubarr program.
package process

import (
	"context"
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

var (
	muErr sync.Mutex
)

// InitProcess begins processing metadata/videos and respective downloads.
func InitProcess(s interfaces.Store, c *models.Channel, videos []*models.Video, ctx context.Context) (bool, []error) {
	conc := c.Settings.Concurrency
	if conc < 1 {
		conc = 1
	}

	// Collect results
	var (
		errors  []error
		success bool
	)

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
		jobs <- video
	}
	close(jobs)

	for i := 0; i < len(videos); i++ {
		if err := <-results; err != nil {
			muErr.Lock()
			errors = append(errors, err)
			muErr.Unlock()
		} else {
			success = true
		}
	}

	if len(errors) > 0 {
		return success, errors
	}
	return success, nil
}

// videoJob starts a worker's process for a video.
func videoJob(id int, videos <-chan *models.Video, results chan<- error, vs interfaces.VideoStore, c *models.Channel, dlTracker *downloads.DownloadTracker, ctx context.Context) {
	for v := range videos {
		var err error

		// Initialize directory parser
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

		if err := processJSON(ctx, v, vs, dlTracker); err != nil {
			results <- fmt.Errorf("JSON processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
			continue
		}

		if logging.Level > 1 {
			fmt.Println()
			logging.I("Worker %d processing: %q", id, v.URL)
			logging.P("Channel ID=%d", v.ChannelID)
			logging.P("Uploaded=%s", v.UploadDate)
		}

		if err := processVideo(ctx, v, vs, dlTracker); err != nil {
			results <- fmt.Errorf("video processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
			continue
		}

		if _, err := exec.LookPath("metarr"); err != nil {
			logging.I("Skipping Metarr process... 'metarr' not available: %v", err)
			continue
		}
		if err := metarr.InitMetarr(v, ctx); err != nil {
			results <- fmt.Errorf("error initializing Metarr: %w", err)
		}
		results <- nil // nil = success
	}
}
