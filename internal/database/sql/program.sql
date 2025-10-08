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