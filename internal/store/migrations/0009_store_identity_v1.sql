-- Revision 4 binds the current store ID to exact canonical identity-document
-- bytes. This schema migration deliberately does not advance format_revision:
-- the Go migration step must generate CSPRNG bytes and atomically install the
-- document, migration receipt, store ID, and final revision.

CREATE TABLE store_identity_v1 (
    singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
    store_id TEXT NOT NULL UNIQUE,
    identity_scheme TEXT NOT NULL CHECK (identity_scheme = 'eventstore-hash-v1'),
    document_bytes BLOB NOT NULL,
    document_digest TEXT NOT NULL,
    artifact_namespace TEXT NOT NULL,
    created_at TEXT NOT NULL,
    creator_protocol TEXT NOT NULL,
    creator_contract_digest TEXT
);

CREATE TABLE store_identity_migration_receipts (
    receipt_id TEXT PRIMARY KEY,
    from_store_id TEXT NOT NULL,
    from_identity_scheme TEXT NOT NULL,
    to_store_id TEXT NOT NULL,
    to_identity_scheme TEXT NOT NULL,
    source_head_digest TEXT NOT NULL,
    source_head_integrity_epoch TEXT NOT NULL,
    source_event_count INTEGER NOT NULL CHECK (source_event_count >= 0),
    source_format_revision INTEGER NOT NULL,
    target_format_revision INTEGER NOT NULL,
    artifact_namespace TEXT NOT NULL,
    backup_database_sha256 TEXT NOT NULL,
    migrated_at TEXT NOT NULL,
    receipt_bytes BLOB NOT NULL,
    receipt_digest TEXT NOT NULL
);
