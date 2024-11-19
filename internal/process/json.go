package process

import (
	"fmt"
	"time"
	"tubarr/internal/command"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// processJSON downloads and processes JSON for a video
func processJSON(v *models.Video, vs interfaces.VideoStore) error {
	if v == nil {
		logging.I("Nil video")
		return nil
	}

	const (
		maxRetries = 3
		maxSleep   = 15 * time.Second
	)

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Create metadata download command
		mdl := command.NewMetaDLRequest(v)
		cmd := mdl.RequestMetaCommand()
		if cmd == nil {
			return fmt.Errorf("failed to create meta DL command for %q", v.URL)
		}

		// Execute metadata download
		if err := command.ExecuteMetaDownload(v, cmd); err != nil {
			lastErr = err
			logging.E(0, "Attempt %d/%d failed downloading metadata for %q: %v",
				attempt, maxRetries, v.URL, err)

			if attempt < maxRetries {
				sleepTime := time.Duration(attempt) * 5 * time.Second

				if sleepTime > maxSleep {
					sleepTime = maxSleep
				}

				logging.D(1, "Waiting %v before retry...", sleepTime)
				time.Sleep(sleepTime)
				continue
			}

			// Log error is retries fail
			logging.E(0, "All %d attempts failed for %q. Final error: %v",
				maxRetries, v.URL, lastErr)

			// Add to database even if metadata download fails after all retries
			logging.D(1, "Adding to database despite metadata failure: %s", v.URL)
			if _, err := vs.AddVideo(v); err != nil {
				return fmt.Errorf("failed to add video to database after metadata failures (%v): %v",
					lastErr, err)
			}
			return nil
		}

		logging.D(1, "Successfully downloaded metadata on attempt %d: %s", attempt, v.URL)
		lastErr = nil
		break
	}

	// If we still have an error after all retries
	if lastErr != nil {
		return fmt.Errorf("failed to download metadata after %d attempts: %v", maxRetries, lastErr)
	}

	// Validate JSON
	valid, err := validateAndStoreJSON(v)
	if err != nil {
		logging.E(0, "JSON validation failed for %q: %v", v.URL, err)
	}

	// Add to database regardless of validation
	if _, err := vs.AddVideo(v); err != nil {
		return fmt.Errorf("failed to add video to database after processing: %v", err)
	}

	if !valid {
		logging.D(1, "JSON validation failed but video added to database: %s", v.URL)
		return nil
	}

	logging.S(0, "Successfully processed metadata for: %s", v.URL)
	return nil
}
