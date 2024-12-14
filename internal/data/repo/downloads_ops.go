package repo

import (
	"context"
	"database/sql"
	"fmt"
	"time"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"
	"tubarr/internal/utils/logging"

	"github.com/Masterminds/squirrel"
)

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

// SetDownloadStatuses updates the download status of an array of videos.
func (ds *DownloadStore) SetDownloadStatuses(videos []*models.Video, status consts.DownloadStatus) error {
	var (
		committed bool
	)

	tx, err := ds.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logging.E(0, "Error rolling back download status for videos %+v: %v", videos, rollbackErr)
			}
		}
	}()

	for _, v := range videos {

		normalizeDownloadStatus(&v.DownloadStatus.Pct, &v.DownloadStatus.Status, v.ID)

		query := squirrel.Update(consts.DBDownloads).
			Set(consts.QDLStatus, v.DownloadStatus.Status).
			Set(consts.QDLPct, v.DownloadStatus.Pct).
			Set(consts.QDLUpdatedAt, time.Now()).
			Where(squirrel.Eq{consts.QVidID: v.ID}).
			RunWith(tx)

		if _, err := query.Exec(); err != nil {
			return fmt.Errorf("failed to update status for video %d: %w", v.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	committed = true
	logging.S(0, "Updated videos statuses %v", videos)
	return nil
}

// SetDownloadStatus updates the download status of a single video.
func (ds *DownloadStore) SetDownloadStatus(v *models.Video) error {
	var (
		committed bool
	)

	tx, err := ds.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logging.E(0, "Error rolling back download status for video with URL %q: %v", v.URL, rollbackErr)
			}
		}
	}()

	query := squirrel.
		Update(consts.DBDownloads).
		Set(consts.QDLStatus, v.DownloadStatus.Status).
		Set(consts.QDLPct, v.DownloadStatus.Pct).
		Set(consts.QDLUpdatedAt, time.Now()).
		Where(squirrel.Eq{consts.QVidID: v.ID}).
		RunWith(tx)

	if _, err := query.Exec(); err != nil {
		return fmt.Errorf("failed to update status for video %d: %w", v.ID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	committed = true
	return nil
}

// UpdateDownloadStatuses retrieves the download status of an array of videos and updates it in place.
func (ds *DownloadStore) UpdateDownloadStatuses(ctx context.Context, updates []models.StatusUpdate) error {
	var committed bool
	tx, err := ds.DB.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if !committed {
			if rollbackErr := tx.Rollback(); rollbackErr != nil {
				logging.E(0, "Failed to rollback transaction for updates: %+v: %v", updates, rollbackErr)
			}
		}
	}()

	// Build batch update query
	query := squirrel.Update(consts.DBDownloads)
	for _, update := range updates {

		normalizeDownloadStatus(&update.Percent, &update.Status, update.VideoID)

		query = query.
			Set(consts.QDLStatus, update.Status).
			Set(consts.QDLPct, update.Percent).
			Where(squirrel.Eq{consts.QDLVidID: update.VideoID}).
			RunWith(tx)
	}

	if _, err := query.ExecContext(ctx); err != nil {
		return fmt.Errorf("failed to update download statuses: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit status updates: %w", err)
	}

	committed = true
	return nil
}

// GetDownloadStatus retrieves the download status of a video.
func (ds *DownloadStore) GetDownloadStatus(v *models.Video) error {

	query := squirrel.Select(consts.QDLVidID, consts.QDLStatus, consts.QDLPct).
		From(consts.DBDownloads).
		Where(squirrel.Eq{consts.QDLVidID: v.ID}).
		RunWith(ds.DB)

	normalizeDownloadStatus(&v.DownloadStatus.Pct, &v.DownloadStatus.Status, v.ID)
	if err := query.QueryRow().Scan(&v); err != nil {
		return fmt.Errorf("failed to query download statuses: %w", err)
	}

	return nil
}

// normalizeDownloadStatus normalizes percentage and statuses if required.
func normalizeDownloadStatus(pctPtr *float64, statusPtr *consts.DownloadStatus, videoID int64) {
	var (
		pct    float64
		status consts.DownloadStatus
	)
	if pctPtr == nil || statusPtr == nil {
		logging.E(0, "Status or percentage passed into function null for video with ID %d", videoID)
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
