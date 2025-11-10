 CREATE TABLE IF NOT EXISTS videos (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_url_id INTEGER REFERENCES channel_urls(id) ON DELETE SET NULL,
    finished INTEGER DEFAULT 0,
    ignored INTEGER DEFAULT 0,
    url_file TEXT,
    url TEXT NOT NULL,
    title TEXT,
    description TEXT,
    thumbnail_url TEXT,
    upload_date TIMESTAMP,
    metadata JSON,
    video_directory TEXT,
    json_directory TEXT,
    video_path TEXT,
    json_path TEXT,
    download_status TEXT DEFAULT "Pending",
    download_pct INTEGER,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(channel_id, url)
);
CREATE INDEX IF NOT EXISTS idx_videos_channel ON videos(channel_id);
CREATE INDEX IF NOT EXISTS idx_videos_url ON videos(url);