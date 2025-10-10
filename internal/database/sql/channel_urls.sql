CREATE TABLE IF NOT EXISTS channel_urls (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL,
    url TEXT NOT NULL,
    username TEXT,
    password TEXT,
    login_url TEXT,
    is_manual INTEGER DEFAULT 0 CHECK(is_manual IN (0, 1)),
    settings JSON,
    metarr JSON,
    last_scan TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE,
    UNIQUE(channel_id, url)
);
CREATE INDEX IF NOT EXISTS idx_channel_urls_channel_id ON channel_urls(channel_id);
CREATE INDEX IF NOT EXISTS idx_channel_urls_url ON channel_urls(url);