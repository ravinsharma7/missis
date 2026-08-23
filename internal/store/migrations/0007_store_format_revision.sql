-- One internal compatibility revision covers all durable event encoding,
-- integrity hashing, projection, and artifact-index behavior. It is separate
-- from the application release and repository revision.
ALTER TABLE store_meta ADD COLUMN format_revision INTEGER NOT NULL DEFAULT 2;
