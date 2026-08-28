-- Revision 5 admits the strict external-ref-v1 durable value vocabulary.
-- No event table column changes are required because typed values live in
-- event_json. The explicit Go migration records the format transition only
-- after its backup and receipt have been created.
CREATE TABLE store_format_migration_receipts (
    receipt_id TEXT PRIMARY KEY,
    source_format_revision INTEGER NOT NULL,
    target_format_revision INTEGER NOT NULL,
    store_id TEXT NOT NULL,
    source_head_digest TEXT NOT NULL,
    source_head_integrity_epoch TEXT NOT NULL,
    source_event_count INTEGER NOT NULL CHECK (source_event_count >= 0),
    backup_database_sha256 TEXT NOT NULL,
    migrated_at TEXT NOT NULL,
    receipt_bytes BLOB NOT NULL,
    receipt_digest TEXT NOT NULL
);
