package repo

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/domain/logger"
	"tubarr/internal/downloads"
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
	tx, err := ds.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Panic rollback failed for video with URL %q: %v", v.URL, rbErr)
			}
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Error rolling back download status for video with URL %q (original error: %v): %v", v.URL, err, rbErr)
			}
		}
	}()

	query := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ?, %s = ? WHERE %s = ?",
		consts.DBDownloads,
		consts.QDLStatus,
		consts.QDLPct,
		consts.QDLUpdatedAt,
		consts.QVidID,
	)

	if _, err := tx.Exec(
		query,
		v.DownloadStatus.Status,
		v.DownloadStatus.Percent,
		time.Now(),
		v.ID,
	); err != nil {
		return fmt.Errorf("failed to update status for video %d: %w", v.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// UpdateDownloadStatus retrieves the download status of a video and updates it in the database.
func (ds *DownloadStore) UpdateDownloadStatus(ctx context.Context, update models.StatusUpdate) error {
	tx, err := ds.DB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Panic rollback failed for updates: %+v: %v", update, rbErr)
			}
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logger.Pl.E("Failed to rollback transaction for updates: %+v (original error: %v): %v", update, err, rbErr)
			}
		}
	}()

	// Execute individual updates within the transaction
	normalizeDownloadStatus(&update.Percent, &update.Status, update.VideoID)

	query := fmt.Sprintf(
		"UPDATE %s SET %s = ?, %s = ? WHERE %s = ?",
		consts.DBDownloads,
		consts.QDLStatus,
		consts.QDLPct,
		consts.QDLVidID,
	)

	if _, err := tx.ExecContext(
		ctx,
		query,
		update.Status,
		update.Percent,
		update.VideoID,
	); err != nil {
		return fmt.Errorf("failed to update download status for video %q: %w", update.VideoURL, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit status updates: %w", err)
	}

	return nil
}

// GetDownloadStatus retrieves the download status of a video.
func (ds *DownloadStore) GetDownloadStatus(v *models.Video) error {

	query := fmt.Sprintf(
		"SELECT %s, %s, %s FROM %s WHERE %s = ?",
		consts.QDLVidID,
		consts.QDLStatus,
		consts.QDLPct,
		consts.DBDownloads,
		consts.QDLVidID,
	)

	normalizeDownloadStatus(&v.DownloadStatus.Percent, &v.DownloadStatus.Status, v.ID)

	if err := ds.DB.QueryRow(query, v.ID).Scan(&v); err != nil {
		return fmt.Errorf("failed to query download statuses: %w", err)
	}

	return nil
}

// CancelDownload cancels an active download by calling the downloads package.
func (ds *DownloadStore) CancelDownload(videoID int64, videoURL string) bool {
	return downloads.CancelDownloadByVideoID(videoID, videoURL)
}

// ******************************** Private ***************************************************************************************

// normalizeDownloadStatus normalizes percentage and statuses if required.
func normalizeDownloadStatus(pctPtr *float64, statusPtr *consts.DownloadStatus, videoID int64) {
	var (
		pct    float64
		status consts.DownloadStatus
	)
	if pctPtr == nil || statusPtr == nil {
		logger.Pl.E("Status or percentage passed into function as 'nil' for video with ID %d", videoID)
		return
	}

	if *pctPtr >= 100.0 {
		status = consts.DLStatusCompleted
		pct = 100.0
	} else if *pctPtr < 0.0 {
		pct = 0.0
	}

	*pctPtr = pct
	*statusPtr = status
}
