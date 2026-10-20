CREATE TABLE IF NOT EXISTS notifications (
    id INTEGER PRIMARY KEY,
    channel_id INTEGER NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    channel_url TEXT NOT NULL DEFAULT '',
    notify_url TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(channel_id, channel_url, notify_url)
);
CREATE INDEX IF NOT EXISTS idx_notification_channel ON notifications(channel_id);
CREATE INDEX IF NOT EXISTS idx_notification_url ON notifications(notify_url);
