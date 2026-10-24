package repo

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/models"
)

// DownloadStore holds a pointer to the sql.DB.
type DownloadStore struct {
	DB *sql.DB
}

// GetDownloadStore returns a channel store instance with injected database.
func GetDownloadStore(db *sql.DB) *DownloadStore {
	return &DownloadStore{
		DB: db,
	}
}

// GetDB returns the database.
func (ds *DownloadStore) GetDB() *sql.DB {
	return ds.DB
}

// SetDownloadStatus updates the download status of a single video.
func (ds *DownloadStore) SetDownloadStatus(v *models.Video) error {
	query := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ?, %s = ? WHERE %s = ?",
		consts.DBDownloads,
		consts.QDLStatus,
		consts.QDLPct,
		consts.QDLUpdatedAt,
		consts.QVidID,
	)

	if _, err := ds.DB.Exec(
		query,
		v.DownloadStatus.Status,
		v.DownloadStatus.Percent,
		time.Now(),
		v.ID,
	); err != nil {
		return fmt.Errorf("failed to update status for video %d: %w", v.ID, err)
	}
	return nil
}

// UpdateDownloadStatus retrieves the download status of a video and updates it in the database.
func (ds *DownloadStore) UpdateDownloadStatus(ctx context.Context, update models.StatusUpdate) error {
	normalizeDownloadStatus(&update.Percent, &update.Status, update.VideoID)

	query := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ?, %s = ? WHERE %s = ?",
		consts.DBDownloads,
		consts.QDLStatus,
		consts.QDLPct,
		consts.QDLError,
		consts.QDLVidID,
	)

	var errMsg string
	if update.Error != nil {
		errMsg = update.Error.Error()
	}
	if _, err := ds.DB.ExecContext(
		ctx,
		query,
		update.Status,
		update.Percent,
		errMsg,
		update.VideoID,
	); err != nil {
		return fmt.Errorf("failed to update download status for video %q: %w", update.VideoURL, err)
	}
	return nil
}

// ******************************** Private ***************************************************************************************

// normalizeDownloadStatus normalizes percentage and statuses if required.
func normalizeDownloadStatus(pctPtr *float64, statusPtr *consts.DownloadStatus, videoID int64) {
	if pctPtr == nil || statusPtr == nil {
		logger.Pl.E("Status or percentage passed into function as 'nil' for video with ID %d", videoID)
		return
	}

	*pctPtr = min(max(*pctPtr, 0.0), 100.0)
	if *statusPtr == consts.DLStatusCompleted {
		*pctPtr = 100.0
	}
}
