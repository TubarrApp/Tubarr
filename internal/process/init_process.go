package process

import (
	"fmt"
	"os/exec"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/parsing"
	"tubarr/internal/utils/logging"
)

// InitProcess begins processing metadata/videos and respective downloads.
func InitProcess(vs models.VideoStore, c *models.Channel, videos []*models.Video) (bool, error) {
	conc := c.Settings.Concurrency
	if conc < 1 {
		conc = 1
	}

	logging.I("Starting meta/video processing for %d videos", len(videos))

	jobs := make(chan *models.Video, len(videos))
	results := make(chan error, len(videos))

	// Start workers
	for w := 1; w <= conc; w++ {
		go videoJob(w, jobs, results, vs, c)
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

	// Collect results
	var (
		errors  []error
		success bool
	)

	for i := 0; i < len(videos); i++ {
		if err := <-results; err != nil {
			errors = append(errors, err)
		} else {
			success = true
		}
	}

	if len(errors) > 0 {
		return success, fmt.Errorf("encountered %d errors during processing: %v", len(errors), errors)
	}
	return success, nil
}

// videoJob starts a worker's process for a video.
func videoJob(id int, videos <-chan *models.Video, results chan<- error, vs models.VideoStore, c *models.Channel) {
	for v := range videos {
		var err error

		// Initialize directory parser
		dirParser := parsing.NewDirectoryParser(c, v)

		var parseDirs = []*string{
			&v.JDir, &v.VDir,
			&c.JDir, &c.VDir,
		}

		for _, ptr := range parseDirs {
			if ptr == nil {
				logging.E(0, "Null pointer")
				continue
			}

			if *ptr, err = dirParser.ParseDirectory(*ptr); err != nil {
				logging.E(0, err.Error())
				continue
			}
		}

		if err := processJSON(v, vs); err != nil {
			results <- fmt.Errorf("JSON processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
			continue
		}

		if logging.Level > 1 {
			fmt.Println()
			logging.I("Worker %d processing: %q", id, v.URL)
			logging.P("Channel ID=%d", v.ChannelID)
			logging.P("Uploaded=%s", v.UploadDate)
		}

		if err := processVideo(v, vs); err != nil {
			results <- fmt.Errorf("video processing error for video (ID: %d, URL: %s): %w", v.ID, v.URL, err)
			continue
		}

		if _, err := exec.LookPath("metarr"); err != nil {
			logging.I("Skipping Metarr process... 'metarr' not available: %v", err)
			continue
		}
		metarr.InitMetarr(v)
		results <- nil // Nil = success
	}
}
