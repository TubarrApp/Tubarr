CREATE TABLE IF NOT EXISTS downloads (
    video_id INTEGER PRIMARY KEY,
    status TEXT DEFAULT 'Queued' NOT NULL,
    percentage REAL DEFAULT 0 NOT NULL CHECK (percentage >= 0 AND percentage <= 100),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(video_id) REFERENCES videos(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_downloads_status ON downloads(status);
-- Status renamed from 'Cancelled'.
UPDATE downloads SET status = 'Canceled' WHERE status = 'Cancelled';