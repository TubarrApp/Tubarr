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
        name TEXT NOT NULL UNIQUE,
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
        channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
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
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        UNIQUE(channel_id, url)
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

// initNotifyTable initializes notification service tables
func initNotifyTable(tx *sql.Tx) error {
	query := `
    CREATE TABLE IF NOT EXISTS notifications (
        id INTEGER PRIMARY KEY,
        channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
        name TEXT NOT NULL,
        notify_url TEXT NOT NULL,
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        UNIQUE(channel_id, notify_url)
    );
    CREATE INDEX IF NOT EXISTS idx_notification_channel ON notifications(channel_id);
    CREATE INDEX IF NOT EXISTS idx_notification_name ON notifications(name);
    CREATE INDEX IF NOT EXISTS idx_notification_url ON notifications(notify_url);
    `
	if _, err := tx.Exec(query); err != nil {
		return fmt.Errorf("failed to create downloads table: %w", err)
	}
	return nil
}
