CREATE TABLE IF NOT EXISTS store_meta (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    store_id TEXT NOT NULL,
    head_hash TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS event_hashes (
    event_id TEXT PRIMARY KEY,
    previous_hash TEXT NOT NULL,
    hash TEXT NOT NULL
);
