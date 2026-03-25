CREATE TABLE IF NOT EXISTS audit_log (
    id             TEXT PRIMARY KEY,
    actor_user_id  TEXT,
    action         TEXT NOT NULL,
    resource_type  TEXT NOT NULL,
    resource_id    TEXT,
    metadata_json  TEXT,
    created_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);
