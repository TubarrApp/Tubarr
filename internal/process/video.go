package process

import (
	"fmt"
	"time"
	"tubarr/internal/command"
	"tubarr/internal/interfaces"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"
)

// processVideo processes video downloads
func processVideo(v *models.Video, vs interfaces.VideoStore) error {
	if v == nil {
		logging.I("No videos to download")
		return nil
	}

	logging.D(2, "Processing download for URL: %s", v.URL)

	vcb := command.NewVideoDLRequest(v)
	cmd, err := vcb.VideoFetchCommand()
	if err != nil {
		return err
	}

	success, err := command.ExecuteVideoDownload(v, cmd)
	if err != nil {
		return err
	}

	if success {
		v.Downloaded = true
		v.UpdatedAt = time.Now()

		// Update the video record
		if err := vs.UpdateVideo(v); err != nil {
			return fmt.Errorf("failed to update video completion status: %w", err)
		}

		logging.D(1, "Successfully processed and marked as downloaded: %s", v.URL)
		return nil
	}
	return fmt.Errorf("video download was unsuccessful")
}
