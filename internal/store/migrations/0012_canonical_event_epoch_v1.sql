-- Revision 7 preserves exact canonical bytes for every newly accepted event
-- and admits an explicit transition from global-json-chain-v1 to
-- canonical-event-chain-v1. Historical rows are not backfilled: their NULL
-- accepted-byte fields prove that the migration did not invent prior input.

ALTER TABLE events ADD COLUMN record_codec TEXT;
ALTER TABLE events ADD COLUMN accepted_bytes BLOB;
ALTER TABLE events ADD COLUMN content_hash TEXT;

ALTER TABLE event_hashes ADD COLUMN integrity_epoch TEXT NOT NULL
    DEFAULT 'global-json-chain-v1';

ALTER TABLE store_meta ADD COLUMN integrity_epoch TEXT NOT NULL
    DEFAULT 'global-json-chain-v1';

CREATE TABLE integrity_epoch_transition_receipts (
    receipt_id TEXT PRIMARY KEY,
    store_id TEXT NOT NULL,
    source_integrity_epoch TEXT NOT NULL,
    source_head_digest TEXT NOT NULL,
    source_event_count INTEGER NOT NULL CHECK (source_event_count >= 0),
    activation_after_alias_seq INTEGER NOT NULL CHECK (activation_after_alias_seq >= 0),
    target_integrity_epoch TEXT NOT NULL,
    record_codec TEXT NOT NULL,
    first_event_id TEXT NOT NULL UNIQUE REFERENCES events(id),
    first_content_hash TEXT NOT NULL,
    first_head_digest TEXT NOT NULL,
    format_revision INTEGER NOT NULL,
    activated_at TEXT NOT NULL,
    receipt_bytes BLOB NOT NULL,
    receipt_digest TEXT NOT NULL
);
