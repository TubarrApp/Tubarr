package process

import (
	"fmt"
	"sync"
	"tubarr/internal/cfg"
	builder "tubarr/internal/command/builder"
	execute "tubarr/internal/command/execute"
	keys "tubarr/internal/domain/keys"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

// ProcessMetaDownloads is the entry point for processing metadata downloads
func ProcessMetaDownloads(dls []*models.DLs) (validRequests []*models.DLs, unwantedURLs []string, err error) {
	if dls == nil {
		return nil, nil, fmt.Errorf("download models passed in null")
	}

	mdl := builder.NewMetaDLRequest(dls)
	mdl.RequestMetaCommand()

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	// Structures and semaphore
	validRequests = make([]*models.DLs, 0, len(dls))
	unwantedURLs = make([]string, 0, len(dls))

	sem := make(chan struct{}, cfg.GetInt(keys.Concurrency))

	for _, dl := range dls {
		wg.Add(1)
		go func(dl *models.DLs) {
			sem <- struct{}{}
			defer func() {
				<-sem
				wg.Done()
			}()

			logging.D(2, "Processing meta download for URL: %s", dl.URL)

			if err := execute.ExecuteMetaDownload(dl); err != nil {
				logging.E(0, "Got error downloading meta for '%s': %v",
					dl.URL, err.Error())
				return
			}

			valid, err := validateJson(dl)
			if err != nil {
				logging.E(0, "JSON validation failed for '%s': %v",
					dl.URL, err.Error())
				return
			}

			if !valid {
				logging.D(2, "Filtered out download for URL '%s'",
					dl.URL)

				mu.Lock()
				unwantedURLs = append(unwantedURLs, dl.URL)
				mu.Unlock()
				return
			}

			mu.Lock()
			logging.D(1, "Got valid download request: %v", dl.URL)
			validRequests = append(validRequests, dl)
			mu.Unlock()
		}(dl)
	}

	// Wait for all downloads to finish
	wg.Wait()

	return validRequests, unwantedURLs, nil
}
