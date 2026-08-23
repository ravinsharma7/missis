# Storage Compatibility Statement

**Status:** store format revision 2 (local alpha) — 2026-08-24

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
- **Artifact metadata:** `artifacts` records durable content-addressed
  objects; blob bytes remain outside SQLite in the configured artifact root.

## Versioning and migration policy

- Schema migrations are **forward-only and additive**. Current set:
  `0001_init`, `0002_link_operation_index`, `0003_store_identity`,
  `0004_projection_snapshots`, `0005_artifacts`, and
  `0006_ordered_parts`, followed by `0007_store_format_revision`.
- Store format is one internal integer independent of binary versions. The
  current value is 2. Stores through 0005 without a marker are implicit
  revision 1; unmarked 0006 stores are implicit revision 2.
- On open, the store: (1) probes compatibility read-only, (2) applies pending
  migrations in order, (3) verifies
  store identity and the hash chain, (4) backfills derived tables for stores
  created before migration 0004.
- Unknown migrations or a revision newer than the binary supports fail before
  WAL setup, migration, integrity verification, or projection repair.
- **Upgrades are in place:** an older store is migrated and backfilled on first
  open by a newer binary. Always back up first
  (`missis-tools backup`, then `missis-tools remote upload`).
- **There is no downgrade path.** Once a store has been opened by a newer
  version, an older binary may not be able to read it. Forward-compatible with
  later versions; not backward-compatible with earlier binaries after
  migration.

### Compatibility corpus

The checked-in `internal/store/testdata/compatibility/revision-0002/` corpus
contains a deterministic database, manifest, and synthetic artifact CAS. It
covers every registered operation, built-in value/inline/reference kind,
relation, first-party ingestion plugin output, provenance shape, temporal
behavior, and derived projection supported by revision 2. It uses fixed IDs,
UTC timestamps, logical slash paths, and synthetic bytes, so tests compare
logical state and hashes consistently on Linux and Windows.

Ordinary `go test ./...` regenerates the corpus in a temporary directory and
checks completeness and freshness. Compatible changes preserve revision 2
and its logical snapshot. Incompatible durable changes increment the revision
and add a retained fixture directory. Never rewrite an accepted fixture in
place. `go run ./tools/store-fixture --output DIR` is the explicit builder.

Confirmed boundary: `v0.2.1` cannot verify revision-2 ledgers containing
ordered events because it did not deserialize `OrderKey` before recomputing
event hashes. Use the paired `v0.2.2` release or newer; this is not evidence of
corruption in such a store.

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
- **Append batches are atomic, including across multiple streams** in one
  transaction (ticket #77): a failed batch writes nothing, and per-stream
  sequences are allocated independently.

## Derived data

- `tickets`/`parts_current` are rebuildable from the ledger via
  `RebuildProjection` (SDK) or the automatic open-time backfill.
- `parts_current.order_key` is a rebuildable projection of the current
  containment event. Empty keys preserve the legacy stream-sequence/Part-ID
  ordering fallback.
- `CheckConsistency` verifies derived tables against the ledger.
- Normal `store.Open` calls own one shared advisory lease for the Store
  lifetime. `OpenWithLease` is reserved for callers that already hold a
  compatible lease, and `OpenSnapshot` is reserved for temporary backup or
  restore databases. These variants share one initialization path so schema,
  hash, and projection behavior cannot drift.
- Lock files can remain after a crash; only the descriptor owns the lock and
  the operating system releases it. Missis does not delete stale PID files or
  wait indefinitely. Migration, GC, and restore fail fast with a structured
  busy error when a shared client is active. Artifact roots use the same
  coordination rule.
- A dense insertion does not append silently. The core first uses a sparse
  decimal midpoint; if no midpoint remains, it assigns fresh sparse keys in
  final order and commits the changed containment events atomically.
- Search remains an in-memory scan over projections; there is no persistent
  search index yet (ticket #52).

## Backup and restore

- New logical backups contain `backup.db`,
  `backup.db.manifest.json`, `backup.db.artifacts/`, and the final
  `backup.db.complete.json` publication marker. The marker is written last
  and binds the database hash, manifest hash, bundle version, and completion
  timestamp.
- `missis-tools backup verify` distinguishes `complete`, `legacy-v1`,
  `incomplete`, and `corrupt` backups. `backup cleanup` removes only stale
  staging paths and explicitly incomplete bundles; it never removes a valid
  published backup automatically.
- `missis-tools backup` writes a consistent copy while clients are active.
  Artifact migration and GC require an exclusive store maintenance lease and
  reject active clients.
- `missis-tools backup` accepts the existing database-only and version-1
  backup formats. Restoring a logical bundle verifies every referenced blob
  before publishing the new database and artifact root. Restore requires a
  new destination and exclusive leases for both destination resources.
- `missis-tools remote upload/download` uses content-addressed keys
  (`<store_id>/<head_hash>.db`), and download verifies store identity, head
  hash, schema version, and event count against a manifest computed from the
  live local store.
- `scripts/verify-restore.sh` verifies a local backup against the live local
  store; pass an explicit backup path or let it derive the current
  content-addressed path.

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

## Artifact roots and migration

- The default new artifact root is the platform user-data directory under
  `missis/artifacts/<hash-of-store-id>/`; the project path is not encoded in
  the root, preventing project collisions and excessive path length.
- `MISSIS_ARTIFACT_STORE` overrides the root for tests and deployments. An
  unusable or overlong root is an explicit error with override guidance.
- Only `<store-directory>/artifacts/` is legacy. Run
  `missis-tools artifacts migrate --store PATH` while all clients are
  stopped. Successful migration quarantines, rather than deletes, the old
  directory. Run `missis-tools artifacts gc --grace DURATION` for explicit
  orphan cleanup; dry-run is the default.

## Ordered inline values

An `inline-sequence` is a typed, ordered value for prose, CodeRef, GitRef,
artifact, image, audio, video, and raw-Markdown items. It is separate from
recursive child Parts and stores references rather than bytes. The core
assigns stable item IDs. Markdown export uses explicit `missis-inline`
markers for typed items; ordinary Markdown media syntax remains inert and is
not promoted during parsing.

Markdown export also emits an inert identity marker before each exported Part:

```markdown
<!-- missis-part {"id":"part:..."} -->
## Evidence
```

The Goldmark-based importer strips and validates this transport metadata,
reuses the identity during re-import, and leaves ordinary comments and
marker-looking content inside code blocks untouched. A top-level document
title is represented by the ticket, so child paths are normalized after that
heading is excluded.

### Markdown transport metadata

Missis transport metadata is an exact, full-line HTML comment. It is not a
code-fence format. Goldmark identifies fenced and indented code blocks first;
marker-looking text owned by those code blocks remains literal Markdown.

Part identity markers have this form and must precede the corresponding
heading, with exporter-generated blank lines allowed:

```markdown
<!-- missis-part {"id":"part:01K2MR7B8Q"} -->
## Evidence
```

The importer treats metadata as follows:

| Input | Result |
| --- | --- |
| No marker | Normal Markdown; the core assigns a new Part ID. |
| Existing Part ID | The current Part identity is reused. |
| Unknown Part ID | A new core ID is assigned and `identity_unresolved` is reported. |
| Missing or empty `id` | Import fails with a line diagnostic. |
| Valid marker not attached to a heading | The marker remains raw Markdown and `identity_unattached` is reported. |
| Duplicate IDs | The import fails atomically. |
| Other HTML comments | They remain ordinary Markdown. |

Concrete identity diagnostics look like this:

```markdown
<!-- missis-part {"id":"part:unknown"} -->
## Imported heading

<!-- missis-part -->
## Missing identity

<!-- missis-part {"id":"part:duplicate"} -->
## First copy
<!-- missis-part {"id":"part:duplicate"} -->
## Second copy

<!-- missis-part {"id":} -->
## Invalid JSON
```

The first record is valid syntax but unresolved against a different target
store, so the core assigns a new ID and reports `identity_unresolved`. The
other records fail atomically with line diagnostics; they are not silently
discarded.

Inline items use the same inert-comment convention:

```markdown
<!-- missis-inline {"ID":"inline-text-001","Kind":"markdown-text","Text":"A human explanation."} -->
<!-- missis-inline {"ID":"inline-image-001","Kind":"image","Data":{"kind":"image","uri":"artifact:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","media_type":"image/png","alt":"diagram"}} -->
<!-- missis-inline {"ID":"inline-audio-001","Kind":"audio","Data":{"kind":"audio","uri":"artifact:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","media_type":"audio/mpeg"}} -->
<!-- missis-inline {"ID":"inline-video-001","Kind":"video","Data":{"kind":"video","uri":"https://example.test/demo.mp4","media_type":"video/mp4"}} -->
<!-- missis-inline {"ID":"inline-artifact-001","Kind":"artifact","Data":{"Ref":{"Kind":"artifact","Entity":"artifact:sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"MediaType":"application/octet-stream","Size":42}} -->
<!-- missis-inline {"ID":"inline-code-001","Kind":"code-ref","Data":{"Repository":"github.com/example/missis","Commit":"abc123","Path":"main.go"}} -->
<!-- missis-inline {"ID":"inline-git-001","Kind":"git-ref","Data":{"Repository":"github.com/example/missis","Commit":"abc123"}} -->
<!-- missis-inline {"ID":"inline-raw-001","Kind":"raw-markdown","Text":"![inert](https://example.test/image.png)"} -->
```

The complete inline kind set is `markdown-text`, `code-ref`, `git-ref`,
`artifact`, `image`, `audio`, `video`, and `raw-markdown`. Marker order is
semantic sequence order. Artifact bytes are never embedded in Markdown;
markers contain only descriptors and references. URLs, Git references, and
raw Markdown media syntax are inert and are never fetched or executed.

Malformed JSON, missing IDs, duplicate IDs, unknown kinds, and missing typed
data produce explicit diagnostics or validation errors. Normal UI views hide
transport comments; raw Markdown export includes them for identity-preserving
round trips.

For example, these are invalid transport records:

```markdown
<!-- missis-inline -->
<!-- missis-inline {"Kind":"image"} -->
<!-- missis-inline {"ID":"x","Kind":"image",} -->
<!-- missis-inline {"ID":"x","Kind":"unknown"} -->
<!-- missis-inline {"ID":"x","Kind":"image","Data":null} -->
<!-- missis-inline {"ID":"x","Kind":"image","Data":{"kind":"image","uri":"not-a-uri"}} -->
```

Inside code, the same text is literal:

````markdown
```markdown
<!-- missis-part {"id":"part:literal-example"} -->
<!-- missis-inline {"ID":"literal-example","Kind":"image"} -->
```
````

An indented code block has the same rule:

```markdown
    <!-- missis-part {"id":"part:indented-literal"} -->
    <!-- missis-inline {"ID":"indented-literal","Kind":"image"} -->
```

The normal UI hides transport comments, but raw Markdown export retains them.
Editing prose is ordinary Markdown editing. Removing a valid Part marker
preserves the text but produces an identity-loss diagnostic; changing the
heading while retaining its marker preserves the Part identity. Structured
media, artifacts, CodeRef, and GitRef changes should use typed API/UI fields
when available, because hand-editing their JSON is intentionally strict.

## Release identity and paired updates

Binary versions remain SemVer while store format uses the independent integer
above. Tagged builds of both binaries report the same full commit and a display
version such as `v0.2.2+g0123456789ab`; local builds report `dev` plus available
Git identity and dirty state.

Stable releases package `missis` and `missis-tools` together. The release
manifest binds both binary hashes and the archive hash/size to one release,
commit, platform, architecture, and store revision. The release installer is
the default path in `scripts/install.sh` and `scripts/install.ps1`. Direct Go
or source installs may be selected explicitly for development, but are not
registered for automatic update.

Self-update refuses split installations, development or dirty builds,
downgrades, malformed manifests, non-HTTPS remote URLs, oversized archives,
unsafe archive paths, checksum failures, and binary identity mismatches. It
stages and verifies both binaries before publication, records rollback state,
and writes `.missis-install.json` last. A later invocation either completes or
rolls back an interrupted update; a persistent journal file alone is not
treated as success.

## Stability promise for revision 2

- The on-disk format is stable while `store_format_revision` remains 2.
- Any future breaking change to the ledger format is gated behind a new
  migration and documented here before release.
- Canonical event payloads (v1) will not be reinterpreted by later phases.

## Local alpha readiness

The release-readiness workflow is intentionally public-surface based:

1. Start with a fresh temporary store and explicit MISSIS_ARTIFACT_STORE.
2. Use missis new, missis set, and missis show for Markdown, typed
   references, and media attachments.
3. Verify ordered traversal, reordering, close/reopen, and projection rebuild.
4. Create and verify a logical backup, restore to a new database and artifact
   root, and run consistency checks.
5. Run missis-tools backup verify before treating the bundle as usable.

The black-box test TestLocalAlphaEndToEndWorkflow exercises this path without
network access. Existing package tests cover concurrent imports, backups,
projection repair, migration, GC, and lease rejection; those tests remain
part of the acceptance evidence rather than being replaced by the smoke test.

The current alpha is local-only. Remote S3-compatible or Harbor storage,
external plugin isolation, asynchronous ingestion, and resumable uploads
remain deferred under tickets #101, #102, #103, and #104. Ticket #93 remains
open while its production rich-renderer and backend follow-ups are incomplete.

## Verification

- `go test ./...` and the black-box suite run in CI on Linux and Windows
  (`.github/workflows/ci.yml`).
- `go run ./tools/check-done` enforces the ticket lifecycle.
