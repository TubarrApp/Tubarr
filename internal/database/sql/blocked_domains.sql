CREATE TABLE IF NOT EXISTS blocked_domains (
    domain TEXT NOT NULL,
    context TEXT NOT NULL CHECK (context IN ('unauth', 'cookie', 'auth')),
    blocked_at TIMESTAMP NOT NULL,
    PRIMARY KEY (domain, context)
);
