package database

import (
	"database/sql"
	"fmt"
)

// initChannelsTable intializes channel tables
func initChannelsTable(tx *sql.Tx) error {
	query := `
    CREATE TABLE IF NOT EXISTS channels (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        url TEXT NOT NULL UNIQUE,
        name TEXT UNIQUE,
        video_directory TEXT,
        json_directory TEXT,
        settings JSON,
        metarr JSON,
        last_scan TIMESTAMP,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );
    CREATE INDEX IF NOT EXISTS idx_channels_url ON channels(url);
    CREATE INDEX IF NOT EXISTS idx_channels_video_directory ON channels(video_directory);
    CREATE INDEX IF NOT EXISTS idx_channels_json_directory ON channels(json_directory);
    CREATE INDEX IF NOT EXISTS idx_channels_name ON channels(name);
    CREATE INDEX IF NOT EXISTS idx_channels_settings ON channels(settings);
    CREATE INDEX IF NOT EXISTS idx_channels_last_scan ON channels(last_scan);
    `
	if _, err := tx.Exec(query); err != nil {
		return fmt.Errorf("failed to create channels table: %w", err)
	}
	return nil
}

// initVideosTable initializes videos tables
func initVideosTable(tx *sql.Tx) error {
	query := `
    CREATE TABLE IF NOT EXISTS videos (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        channel_id INTEGER REFERENCES channels(id),
        downloaded INTEGER DEFAULT 0,
        url TEXT NOT NULL UNIQUE,
        title TEXT,
        description TEXT,
        video_directory TEXT,
        json_directory TEXT,
        upload_date TIMESTAMP,
        metadata JSON,
        settings JSON,
        metarr JSON,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );
    CREATE INDEX IF NOT EXISTS idx_videos_channel ON videos(channel_id);
    CREATE INDEX IF NOT EXISTS idx_videos_url ON videos(url);
    CREATE INDEX IF NOT EXISTS idx_videos_upload_date ON videos(upload_date);
    `
	if _, err := tx.Exec(query); err != nil {
		return fmt.Errorf("failed to create videos table: %w", err)
	}
	return nil
}

// initDownloadsTable initializes downloads table
func initDownloadsTable(tx *sql.Tx) error {
	query := `
    CREATE TABLE IF NOT EXISTS downloads (
        id INTEGER PRIMARY KEY,
        video_id INTEGER REFERENCES videos(id),
        status TEXT NOT NULL CHECK(status IN ('pending', 'downloading', 'completed', 'failed')),
        file_path TEXT,
        file_size INTEGER,
        started_at TIMESTAMP,
        completed_at TIMESTAMP,
        error_message TEXT,
        settings_used JSON,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
    );
    CREATE INDEX IF NOT EXISTS idx_downloads_status ON downloads(status);
    CREATE INDEX IF NOT EXISTS idx_downloads_video ON downloads(video_id);
    `
	if _, err := tx.Exec(query); err != nil {
		return fmt.Errorf("failed to create downloads table: %w", err)
	}
	return nil
}
