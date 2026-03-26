CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY NOT NULL,
    value TEXT NOT NULL
);
INSERT OR IGNORE INTO settings (key, value) VALUES ('crawl-concurrency', '0');
INSERT OR IGNORE INTO settings (key, value) VALUES ('global-download-concurrency', '0');
INSERT OR IGNORE INTO settings (key, value) VALUES ('domain-download-limits', '{}');
