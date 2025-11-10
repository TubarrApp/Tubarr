package server

import (
	"database/sql"
	"fmt"
	"tubarr/internal/domain/consts"
	"tubarr/internal/models"

	"github.com/Masterminds/squirrel"
)

// getHomepageCarouselVideos returns the latest 'n' downloaded videos.
func (s *serverStore) getHomepageCarouselVideos(channel *models.Channel, n int) (videos []models.Video, err error) {
	query := squirrel.
		Select(
			consts.QVidID,
			consts.QVidChanID,
			consts.QVidChanURLID,
			consts.QVidThumbnailURL,
			consts.QVidFinished,
			consts.QVidURL,
			consts.QVidTitle,
			consts.QVidDescription,
			consts.QVidUploadDate,
			consts.QVidCreatedAt,
			consts.QVidUpdatedAt,
		).
		From(consts.DBVideos).
		Where(squirrel.Eq{consts.QVidChanID: channel.ID}).
		Where(squirrel.Eq{consts.QVidFinished: 1}).            // Only finished downloads
		OrderBy(fmt.Sprintf("%s DESC", consts.QVidUpdatedAt)). // Most recent first
		Limit(uint64(n))

	sqlPlaceholder, args, err := query.PlaceholderFormat(squirrel.Question).ToSql()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	rows, err := s.db.Query(sqlPlaceholder, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query videos: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var video models.Video
		var thumbnailURL sql.NullString
		var description sql.NullString
		var uploadDate sql.NullTime
		var channelURLID sql.NullInt64

		err := rows.Scan(
			&video.ID,
			&video.ChannelID,
			&channelURLID,
			&thumbnailURL,
			&video.Finished,
			&video.URL,
			&video.Title,
			&description,
			&uploadDate,
			&video.CreatedAt,
			&video.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan video row: %w", err)
		}

		// Handle nullable fields
		if channelURLID.Valid {
			video.ChannelURLID = channelURLID.Int64
		}
		if thumbnailURL.Valid {
			video.ThumbnailURL = thumbnailURL.String
		}
		if description.Valid {
			video.Description = description.String
		}
		if uploadDate.Valid {
			video.UploadDate = uploadDate.Time
		}
		videos = append(videos, video)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return videos, nil
}
