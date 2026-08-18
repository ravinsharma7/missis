CREATE TABLE IF NOT EXISTS streams (
    stream_kind TEXT NOT NULL,
    stream_entity TEXT NOT NULL,
    next_sequence INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (stream_kind, stream_entity)
);

CREATE TABLE IF NOT EXISTS events (
    alias_seq INTEGER PRIMARY KEY AUTOINCREMENT,
    id TEXT NOT NULL UNIQUE,
    stream_kind TEXT NOT NULL,
    stream_entity TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    event_json TEXT NOT NULL,
    UNIQUE (stream_kind, stream_entity, sequence)
);

CREATE INDEX IF NOT EXISTS idx_events_stream ON events(stream_kind, stream_entity);

CREATE TABLE IF NOT EXISTS idempotency (
    key TEXT PRIMARY KEY,
    event_ids_json TEXT NOT NULL,
    result_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ticket_aliases (
    number INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id TEXT NOT NULL UNIQUE
);
