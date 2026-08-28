-- Revision 6 admits artifact-namespace-fork-v1 manifests and completion
-- markers plus store-identity-fork-v2 receipts. The indexed row makes the
-- filesystem publication and the identity change independently inspectable.
CREATE TABLE artifact_namespace_forks (
    receipt_id TEXT PRIMARY KEY REFERENCES store_identity_migration_receipts(receipt_id),
    from_store_id TEXT NOT NULL,
    to_store_id TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    completion_marker_digest TEXT NOT NULL,
    copied_object_count INTEGER NOT NULL CHECK (copied_object_count >= 0),
    copied_byte_count INTEGER NOT NULL CHECK (copied_byte_count >= 0),
    unmanaged_reference_count INTEGER NOT NULL CHECK (unmanaged_reference_count >= 0),
    excluded_object_count INTEGER NOT NULL CHECK (excluded_object_count >= 0),
    completed_at TEXT NOT NULL
);
