CREATE TABLE IF NOT EXISTS api_keys (
    id          TEXT PRIMARY KEY,
    key_hash    TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    team_id     TEXT REFERENCES teams(id),
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    last_used_at DATETIME
);
