package process

import (
	"sync"
	"tubarr/internal/cfg"
	build "tubarr/internal/command/builder"
	execute "tubarr/internal/command/execute"
	keys "tubarr/internal/domain/keys"
	"tubarr/internal/models"
	logging "tubarr/internal/utils/logging"
)

// ProcessVideoDownloads processes video request downloads
func ProcessVideoDownloads(dls []*models.DLs) ([]*models.DLs, []string, bool) {
	maxConcurrent := cfg.GetInt(keys.Concurrency)

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	// Create channels for the worker pool
	jobs := make(chan *models.DLs, len(dls))

	validUrls := make([]string, 0, len(dls))
	successful := make([]*models.DLs, 0, len(dls))

	// Start workers
	for i := 0; i < maxConcurrent; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for dl := range jobs {
				logging.D(2, "Worker %d processing download for URL: %s", workerID, dl.URL)

				vcb := build.NewVideoDLRequest(dl)
				if err := vcb.VideoFetchCommand(); err != nil {
					logging.E(0, "Worker %d: %v", workerID, err.Error())
					continue
				}

				success, err := execute.ExecuteVideoDownload(dl)
				if err != nil {
					logging.E(0, "Worker %d: %v", workerID, err.Error())
					continue
				}

				if success {
					mu.Lock()
					validUrls = append(validUrls, dl.URL)
					successful = append(successful, dl)
					mu.Unlock()

					logging.D(1, "Worker %d successfully processed: %s", workerID, dl.URL)
				}
			}
		}(i)
	}

	// Send jobs to workers
	for _, dl := range dls {
		if dl != nil {
			jobs <- dl
		}
	}
	close(jobs)

	// Wait for all workers to complete
	wg.Wait()

	if len(validUrls) == 0 {
		return nil, nil, false
	}

	return successful, validUrls, true
}
