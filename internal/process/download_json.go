package process

import (
	"sync"
	builder "tubarr/internal/command/builder"
	execute "tubarr/internal/command/execute"
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

	var (
		wg sync.WaitGroup
		mu sync.Mutex
	)

	validDls := make([]*models.DLs, 0, len(dls))

	for _, dl := range dls {

		if dl == nil {
			logging.E(0, "Dl found nil")
			continue
		}

		wg.Add(1)
		go func(dl *models.DLs) {
			defer wg.Done()

			if err := execute.ExecuteMetaDownload(dl); err != nil {
				logging.E(0, "Got error downloading meta for '%s': %v", dl.URL, err.Error())
				return
			}

			valid, err := validateJson(dl)
			if err != nil {
				logging.E(0, "JSON validation failed for '%s': %v", dl.URL, err.Error())
				return
			}
			if !valid {
				logging.D(2, "Filtered out download for URL '%s'", dl.URL)
				return
			}

			mu.Lock()
			logging.D(1, "Got valid download request: %v", dl.URL)
			validDls = append(validDls, dl)
			mu.Unlock()
		}(dl)
	}
	wg.Wait()

	return validDls
}
