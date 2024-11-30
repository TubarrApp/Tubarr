package database

import (
	"database/sql"
	"fmt"
)

// initProgramTable initializes the primary program database table.
func initProgramTable(tx *sql.Tx) error {
	query := `
    CREATE TABLE IF NOT EXISTS program (
        id INTEGER PRIMARY KEY CHECK (id = 1),
        program_id TEXT UNIQUE NOT NULL,
        running INTEGER DEFAULT 0,
        pid INTEGER,
        started_at TIMESTAMP,
        last_heartbeat TIMESTAMP,
        host TEXT
        CONSTRAINT single_row CHECK (id = 1)
    );
    INSERT OR IGNORE INTO program (id, program_id, running) VALUES (1, 'Tubarr', 0);
    `
	if _, err := tx.Exec(query); err != nil {
		return fmt.Errorf("failed to create program table: %w", err)
	}
	return nil
}

// initChannelsTable intializes channel tables.
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
    CREATE INDEX IF NOT EXISTS idx_channels_name ON channels(name);
    CREATE INDEX IF NOT EXISTS idx_channels_last_scan ON channels(last_scan);
    `
	if _, err := tx.Exec(query); err != nil {
		return fmt.Errorf("failed to create channels table: %w", err)
	}
	return nil
}

// initNotifyTable initializes notification service tables.
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
    CREATE INDEX IF NOT EXISTS idx_notification_url ON notifications(notify_url);
    `
	if _, err := tx.Exec(query); err != nil {
		return fmt.Errorf("failed to create downloads table: %w", err)
	}
	return nil
}

// initVideosTable initializes videos tables.
func initVideosTable(tx *sql.Tx) error {
	query := `
    CREATE TABLE IF NOT EXISTS videos (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
        downloaded INTEGER DEFAULT 0,
        url TEXT NOT NULL,
        title TEXT,
        description TEXT,
        video_directory TEXT,
        json_directory TEXT,
        video_path TEXT,
        json_path TEXT,
        download_status TEXT DEFAULT "Pending",
        download_pct INTEGER,
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
    `
	if _, err := tx.Exec(query); err != nil {
		return fmt.Errorf("failed to create videos table: %w", err)
	}
	return nil
}

// initDownloadsTable initializes the download status table for videos.
func initDownloadsTable(tx *sql.Tx) error {
	query := `
    CREATE TABLE IF NOT EXISTS downloads (
        video_id INTEGER PRIMARY KEY,
        status TEXT DEFAULT 'Pending' NOT NULL,
        percentage REAL DEFAULT 0 NOT NULL CHECK (percentage >= 0 AND percentage <= 100),
        created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
        FOREIGN KEY(video_id) REFERENCES videos(id) ON DELETE CASCADE
    );

    CREATE INDEX IF NOT EXISTS idx_downloads_status ON downloads(status);
    `
	if _, err := tx.Exec(query); err != nil {
		return fmt.Errorf("failed to create downloads table: %w", err)
	}
	return nil
}
