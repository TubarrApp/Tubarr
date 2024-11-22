package process

import (
	"fmt"
	"os/exec"
	"sync"
	"time"
	"tubarr/internal/metarr"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// InitProcess begins processing metadata/videos and respective downloads.
func InitProcess(vs models.VideoStore, c *models.Channel, videos []*models.Video) error {
	var (
		wg      sync.WaitGroup
		errChan = make(chan error, len(videos))
	)

	logging.I("Starting meta/video processing for %d videos", len(videos))

	conc := c.Settings.Concurrency
	if conc < 1 {
		conc = 1
	}

	taskChan := make(chan *models.Video, len(videos))

	// Add "conc" to the wait group count
	wg.Add(conc)
	for i := 0; i < conc; i++ {
		go func() {
			defer wg.Done()
			for v := range taskChan {
				var err error
				if v.ID, err = vs.AddVideo(v); err != nil {
					errChan <- fmt.Errorf("error adding video: %w", err)
					continue
				}

				if err := processJSON(v, vs); err != nil {
					errChan <- fmt.Errorf("JSON processing error for %s: %w", v.URL, err)
					continue
				}

				if logging.Level > 1 {
					fmt.Println()
					logging.I("Processing video: %q", v.URL)
					logging.P("Channel ID=%d", v.ChannelID)
					logging.P("Created At=%s\n", v.CreatedAt.Format(time.RFC822Z))
				}

				if err := processVideo(v, vs); err != nil {
					errChan <- fmt.Errorf("video processing error for %s: %w", v.URL, err)
					continue
				}

				if _, err := exec.LookPath("metarr"); err != nil {
					logging.I("Skipping Metarr process... 'metarr' not available: %v", err)
					continue
				}
				metarr.InitMetarr(v)
			}
		}()
	}

	// Send work to workers
	for i, video := range videos {
		if video == nil {
			logging.E(0, "Video %d/%d in queue for channel %q is nil", i, len(videos), c.Name)
			continue
		}
		taskChan <- video
	}

	close(taskChan) // Close channel after all videos are enqueued
	wg.Wait()       // Wait for all workers to finish
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
