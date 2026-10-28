CREATE TABLE IF NOT EXISTS blocked_domains (
    domain TEXT NOT NULL,
    context TEXT NOT NULL CHECK (context IN ('unauth', 'cookie', 'auth')),
    blocked_at TIMESTAMP NOT NULL,
    PRIMARY KEY (domain, context)
);

CREATE TABLE IF NOT EXISTS blocked_domain_videos (
    domain TEXT NOT NULL,
    context TEXT NOT NULL CHECK (context IN ('unauth', 'cookie', 'auth')),
    video_id INTEGER NOT NULL,
    channel_id INTEGER NOT NULL,
    PRIMARY KEY (domain, context, video_id),
    FOREIGN KEY (domain, context)
        REFERENCES blocked_domains (domain, context)
        ON DELETE CASCADE
);