 CREATE TABLE IF NOT EXISTS videos (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    -- downloaded INTEGER DEFAULT 0, (Possibly redundant, may just use download_status now?)
    url_file TEXT,
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