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
func ProcessVideoDownloads(dls []*models.DLs) (successfulDLs []*models.DLs, validURLs []string, done bool) {

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	// Channels and sem
	validURLs = make([]string, 0, len(dls))
	successfulDLs = make([]*models.DLs, 0, len(dls))

	sem := make(chan struct{}, cfg.GetInt(keys.Concurrency))

	// Initialize sem
	for _, dl := range dls {
		if dl == nil {
			logging.E(0, "DL request found nil, skipping...")
			continue
		}

		wg.Add(1)
		go func(dl *models.DLs) {
			sem <- struct{}{}
			defer func() {
				<-sem
				wg.Done()
			}()

			logging.D(2, "Processing download for URL: %s", dl.URL)

			vcb := build.NewVideoDLRequest(dl)
			if err := vcb.VideoFetchCommand(); err != nil {
				logging.E(0, err.Error())
				return
			}

			success, err := execute.ExecuteVideoDownload(dl)
			if err != nil {
				logging.E(0, err.Error())
				return
			}

			if success {
				mu.Lock()
				validURLs = append(validURLs, dl.URL)
				successfulDLs = append(successfulDLs, dl)
				mu.Unlock()

				logging.D(1, "Successfully processed: %s", dl.URL)
			}
		}(dl)
	}

	// Wait for all downloads to complete
	wg.Wait()

	if len(validURLs) == 0 {
		return nil, nil, false
	}

	return successfulDLs, validURLs, true
}
