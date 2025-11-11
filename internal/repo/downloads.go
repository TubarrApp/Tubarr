package repo

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/downloads"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/Masterminds/squirrel"
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
				logging.E("Panic rollback failed for video with URL %q: %v", v.URL, rbErr)
			}
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logging.E("Error rolling back download status for video with URL %q (original error: %v): %v", v.URL, err, rbErr)
			}
		}
	}()

	query := squirrel.
		Update(consts.DBDownloads).
		Set(consts.QDLStatus, v.DownloadStatus.Status).
		Set(consts.QDLPct, v.DownloadStatus.Percent).
		Set(consts.QDLUpdatedAt, time.Now()).
		Where(squirrel.Eq{consts.QVidID: v.ID}).
		RunWith(tx)

	if _, err := query.Exec(); err != nil {
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
				logging.E("Panic rollback failed for updates: %+v: %v", update, rbErr)
			}
			panic(p)
		} else if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				logging.E("Failed to rollback transaction for updates: %+v (original error: %v): %v", update, err, rbErr)
			}
		}
	}()

	// Execute individual updates within the transaction
	normalizeDownloadStatus(&update.Percent, &update.Status, update.VideoID)

	query := squirrel.Update(consts.DBDownloads).
		Set(consts.QDLStatus, update.Status).
		Set(consts.QDLPct, update.Percent).
		Where(squirrel.Eq{consts.QDLVidID: update.VideoID}).
		RunWith(tx)

	if _, err := query.ExecContext(ctx); err != nil {
		return fmt.Errorf("failed to update download status for video %q: %w", update.VideoURL, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit status updates: %w", err)
	}

	return nil
}

// GetDownloadStatus retrieves the download status of a video.
func (ds *DownloadStore) GetDownloadStatus(v *models.Video) error {

	query := squirrel.Select(consts.QDLVidID, consts.QDLStatus, consts.QDLPct).
		From(consts.DBDownloads).
		Where(squirrel.Eq{consts.QDLVidID: v.ID}).
		RunWith(ds.DB)

	normalizeDownloadStatus(&v.DownloadStatus.Percent, &v.DownloadStatus.Status, v.ID)
	if err := query.QueryRow().Scan(&v); err != nil {
		return fmt.Errorf("failed to query download statuses: %w", err)
	}

	return nil
}

// CancelDownload cancels an active download by calling the downloads package.
func (ds *DownloadStore) CancelDownload(videoID int64, videoURL string) bool {
	return downloads.CancelDownloadByVideoID(videoID, videoURL)
}

// ******************************** Private ********************************

// normalizeDownloadStatus normalizes percentage and statuses if required.
func normalizeDownloadStatus(pctPtr *float64, statusPtr *consts.DownloadStatus, videoID int64) {
	var (
		pct    float64
		status consts.DownloadStatus
	)
	if pctPtr == nil || statusPtr == nil {
		logging.E("Status or percentage passed into function as 'nil' for video with ID %d", videoID)
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
