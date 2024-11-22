package process

import (
	"fmt"
	"os/exec"
	"sync"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// InitProcess begins processing metadata/videos and respective downloads.
func InitProcess(vs models.VideoStore, c *models.Channel, videos []*models.Video) error {
	var (
		wg      sync.WaitGroup
		errChan = make(chan error, len(videos))
		err     error
	)

	logging.I("Starting meta/video processing for %d videos", len(videos))

	conc := c.Settings.Concurrency
	if conc < 1 {
		conc = 1
	}
	sem := make(chan struct{}, conc)

	for _, video := range videos {
		if video == nil {
			logging.E(0, "Video is null")
			continue
		}
		wg.Add(1)

		go func(v *models.Video) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			if v.ID, err = vs.AddVideo(v); err != nil {
				errChan <- err
			}

			if err := processJSON(v, vs); err != nil {
				errChan <- fmt.Errorf("JSON processing error for %s: %w", v.URL, err)
				return
			}

			if logging.Level > 1 {
				fmt.Println()
				logging.I("Got requests for %q", v.URL)
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
			metarr.InitMetarr(v)
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
