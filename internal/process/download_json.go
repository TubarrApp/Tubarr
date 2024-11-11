package process

import (
	"sync"
	"tubarr/internal/cfg"
	builder "tubarr/internal/command/builder"
	execute "tubarr/internal/command/execute"
	keys "tubarr/internal/domain/keys"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

// ProcessMetaDownloads is the entry point for processing metadata downloads
func ProcessMetaDownloads(dls []*models.DLs) []*models.DLs {
	if dls == nil {
		logging.E(0, "Dls passed in nil")
		return nil
	}

	mdl := builder.NewMetaDLRequest(dls)
	mdl.RequestMetaCommand()

	maxConcurrent := cfg.GetInt(keys.Concurrency)

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	// Worker channel and valid download array
	jobs := make(chan *models.DLs, len(dls))
	validDls := make([]*models.DLs, 0, len(dls))

	// Start workers
	for i := 0; i < maxConcurrent; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for dl := range jobs {
				if dl == nil {
					logging.E(0, "Worker %d: Dl found nil", workerID)
					continue
				}

				logging.D(2, "Worker %d processing metadata for URL: %s", workerID, dl.URL)

				if err := execute.ExecuteMetaDownload(dl); err != nil {
					logging.E(0, "Worker %d: Got error downloading meta for '%s': %v",
						workerID, dl.URL, err.Error())
					continue
				}

				valid, err := validateJson(dl)
				if err != nil {
					logging.E(0, "Worker %d: JSON validation failed for '%s': %v",
						workerID, dl.URL, err.Error())
					continue
				}

				if !valid {
					logging.D(2, "Worker %d: Filtered out download for URL '%s'",
						workerID, dl.URL)
					continue
				}

				mu.Lock()
				logging.D(1, "Worker %d: Got valid download request: %v", workerID, dl.URL)
				validDls = append(validDls, dl)
				mu.Unlock()
			}
		}(i)
	}

	// Send jobs to workers
	for _, dl := range dls {
		jobs <- dl
	}
	close(jobs)
	wg.Wait()

	return validDls
}
