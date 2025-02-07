CREATE TABLE IF NOT EXISTS channel_urls (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    channel_id INTEGER NOT NULL,
    url TEXT NOT NULL UNIQUE,
    username TEXT,
    password TEXT,
    login_url TEXT,
    FOREIGN KEY (channel_id) REFERENCES channels(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_channel_urls_channel_id ON channel_urls(channel_id);
CREATE INDEX IF NOT EXISTS idx_channel_urls_url ON channel_urls(url);