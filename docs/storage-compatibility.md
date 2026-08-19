# Storage Compatibility Statement

**Status:** v0.1.0 (alpha) — 2026-08-18

This statement is the compatibility contract for the on-disk store format.
It exists so upgrades and cross-platform moves are predictable. Ticket: #53.

**Source of truth:** the authoritative contract is
`specs/missues-issue-specification.v2.md`; this document is the human-readable
compatibility summary. Any ticket that changes the on-disk format (new
migration, canonical encoding, derived tables) must update this document.

## What a missis store is

A single SQLite database file, holding:

- **The event ledger (source of truth):** `events` with
  `stream_kind`/`stream_entity`/`sequence`/`event_json`/`alias_seq`, plus the
  per-event hash chain in `event_hashes` and the chain head in `store_meta`.
- **Derived current projections:** `tickets` and `parts_current` — rebuildable
  current-time snapshots maintained transactionally on append (ticket #51).
- **Idempotency records:** `idempotency` mapping a client key to the event IDs
  and JSON result it produced.
- **Migration bookkeeping:** `schema_migrations` recording applied versions.

## Versioning and migration policy

- Schema migrations are **forward-only and additive**. Current set:
  `0001_init`, `0002_link_operation_index`, `0003_store_identity`,
  `0004_projection_snapshots`.
- On open, the store: (1) applies pending migrations in order, (2) verifies
  store identity and the hash chain, (3) backfills derived tables for stores
  created before migration 0004.
- **Upgrades are in place:** an older store is migrated and backfilled on first
  open by a newer binary. Always back up first
  (`tools/store-backup`, then `tools/store-remote upload`).
- **There is no downgrade path.** Once a store has been opened by a newer
  version, an older binary may not be able to read it. Forward-compatible with
  later versions; not backward-compatible with earlier binaries after
  migration.

## Integrity contract

- **Canonical event encoding v1** (ticket #45) is used for hash computation;
  timestamps are 9-digit nanoseconds UTC.
- **Head hash** is a SHA-256 chain over canonical events. It is verified on
  every open (an intentional O(ledger) integrity check, ticket #51 decision)
  and by `show --health` / `CheckConsistency`.
- **Store identity** (`store_id`) is immutable; **per-stream sequences** are
  unique and strictly increasing. A sequence gap is an integrity incident:
  accepted events are never rewritten, and recovery is restore-from-backup.
- **Bitemporal semantics** (ticket #42): the winner is
  `max(effective_at, recorded_at, stream_sequence, event_id)` among candidates
  with `recorded_at <= K` and `effective_at <= V`; boundaries are inclusive;
  retraction opens an interval hole; supersession voids as of known time.

## Derived data

- `tickets`/`parts_current` are rebuildable from the ledger via
  `RebuildProjection` (SDK) or the automatic open-time backfill.
- `CheckConsistency` verifies derived tables against the ledger.
- Search remains an in-memory scan over projections; there is no persistent
  search index yet (ticket #52).

## Backup and restore

- `tools/store-backup` writes a consistent copy.
- `tools/store-remote upload/download/verify` uses content-addressed keys
  (`<store_id>/<head_hash>.db`), and download verifies store identity, head
  hash, schema version, and event count against the local manifest.
- `tools/verify-restore.sh` verifies a local backup the same way.

## Relation vocabulary

- Relation names are payload text inside link events (`Value.Text`), so a
  vocabulary change is a ledger-format concern, not just a code rename.
- v0.1.x vocabulary change (2026-08-19, ticket #28): `home-project` was
  renamed to `has-home` (asserted) with inverse `home-of`. No command created
  `home-project` links before the rename, so existing stores are not
  expected to contain the old name; if one is found, treat it as an unknown
  relation until explicitly migrated.
- Future vocabulary changes follow the versioning and migration policy above.

## Cross-platform behavior

- The store file format is identical across operating systems; only the
  **default location** differs: `%LOCALAPPDATA%\missis\missis.db` on Windows
  (with `os.UserConfigDir()` and legacy XDG fallbacks), and
  `~/.local/share/missis/missis.db` on POSIX (ticket #55).
- Private-by-default permissions are POSIX-scoped (0700 dir / 0600 file);
  Windows relies on user-profile ACLs.
- Remote object keys keep the `store:` prefix for rclone/aws; the local
  filesystem remote sanitizes the colon so keys are valid Windows paths.

## Stability promise for v0.1.0

- The on-disk format is stable within the v0.1.x series.
- Any future breaking change to the ledger format is gated behind a new
  migration and documented here before release.
- Canonical event payloads (v1) will not be reinterpreted by later phases.

## Verification

- `go test ./...` and the black-box suite run in CI on Linux and Windows
  (`.github/workflows/ci.yml`).
- `go run ./tools/check-done` enforces the ticket lifecycle.
