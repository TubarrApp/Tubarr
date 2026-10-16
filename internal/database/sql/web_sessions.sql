CREATE TABLE IF NOT EXISTS web_sessions (
    token_hash TEXT PRIMARY KEY NOT NULL,
    expires_at DATETIME NOT NULL
);
