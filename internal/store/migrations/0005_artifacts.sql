-- Artifact metadata index (ticket #93). Blob bytes remain in the configured
-- artifact backend; this table is a rebuildable/indexable record of durable
-- identity and metadata, not a copy of the content.
CREATE TABLE artifacts (
  ref         TEXT PRIMARY KEY,
  algorithm   TEXT NOT NULL,
  digest      TEXT NOT NULL UNIQUE,
  media_type  TEXT NOT NULL DEFAULT '',
  size        INTEGER NOT NULL,
  backend     TEXT NOT NULL,
  recorded_at TEXT NOT NULL
);

CREATE INDEX idx_artifacts_media_type ON artifacts(media_type);
