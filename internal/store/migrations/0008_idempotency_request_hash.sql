-- Revision 3 gives new idempotency receipts a mandatory request fingerprint.
-- Format-v2 rows cannot be rebound safely because their caller request is not
-- recoverable from accepted events/result JSON. Move them out of the active
-- receipt table into permanent key tombstones: audit stays possible and an
-- old retry key can never look unused and execute again.

CREATE TABLE idempotency_v3 (
    key TEXT PRIMARY KEY,
    event_ids_json TEXT NOT NULL,
    result_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    request_hash TEXT NOT NULL
);

CREATE TABLE idempotency_key_tombstones (
    key TEXT PRIMARY KEY,
    event_ids_json TEXT NOT NULL,
    result_json TEXT NOT NULL,
    created_at TEXT NOT NULL,
    retired_at TEXT NOT NULL,
    reason TEXT NOT NULL CHECK (reason = 'format-v2-unbound-request')
);

INSERT INTO idempotency_key_tombstones(
    key, event_ids_json, result_json, created_at, retired_at, reason
)
SELECT key, event_ids_json, result_json, created_at,
       strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
       'format-v2-unbound-request'
FROM idempotency;

DROP TABLE idempotency;
ALTER TABLE idempotency_v3 RENAME TO idempotency;

UPDATE store_meta
SET format_revision = 3,
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE singleton = 1;
