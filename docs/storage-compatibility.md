# Storage Compatibility Statement

**Status:** store format revision 7 (local alpha) — 2026-08-28

This statement is the compatibility contract for the on-disk store format.
It exists so upgrades and cross-platform moves are predictable. Ticket: #53.

`store_format_revision=7` is a Missis SQLite compatibility number. It is not
the consumer-neutral event-store v3-alpha protocol. The latter is the active
alpha extraction contract in
`specs/event-store-v3-alpha.md` for separate Missis, Spy Testing, and CSS
Flight Recorder adapters.

**Sources of truth:** `specs/missues-issue-specification.v2.md` is the frozen
Missis product/domain contract; `specs/event-store-v3-alpha.md` and
`specs/cross-store-references-v3-alpha.md` govern new event-store behavior.
This document is the human-readable compatibility summary. Any ticket that changes the on-disk format (new
migration, canonical encoding, derived tables) must update this document.

## Binary/store rollout generations

Changing the installed release and changing a store format are separately
durable operations. Use this generation rollout whenever the target release
has a different normal-open format:

~~~text
inspect
  -> stage and verify both target binaries
  -> plan with the staged maintenance tool
  -> stop clients and acquire the exclusive lease
  -> re-plan
  -> create and verify the rollback generation
  -> migrate the explicitly named store
  -> verify using the staged pair
  -> activate both binaries
  -> write the installation manifest last
  -> re-inspect and commit the rollout journal
~~~

The rollback generation consists of the previous `missis` and
`missis-tools` binaries, their installation manifest, and the verified
pre-migration database/artifact backup. Do not mix components across
generations.

For the repository incident recorded on 2026-08-28, the store is already
format 6 and contains work accepted after migration. Do not restore the older
database merely to satisfy `v0.2.2`. Until a stable format-6 pair is
published, use the current Linux source explicitly:

~~~bash
go run ./tools/missis-tools tui --store ./.missis-store/missis.db
~~~

Do not use the Windows executable through `\\wsl.localhost` for this store.
That is an unsupported cross-OS SQLite access path, not a migration method.

### Exact rollout commands

Use the installer from the exact target tag. The target format is mandatory;
never use an unversioned `--migrate` switch.

~~~bash
go run github.com/ravinsharma7/missis/tools/paired-install@vX.Y.Z \
  --ref vX.Y.Z \
  --bin-dir /absolute/bin \
  --rollout plan \
  --store /absolute/project/.missis-store/missis.db \
  --to-format N \
  --backup /absolute/backups/pre-format-N.db \
  --json

go run github.com/ravinsharma7/missis/tools/paired-install@vX.Y.Z \
  --ref vX.Y.Z \
  --bin-dir /absolute/bin \
  --rollout apply \
  --store /absolute/project/.missis-store/missis.db \
  --to-format N \
  --backup /absolute/backups/pre-format-N.db \
  --json
~~~

`plan` downloads and verifies the target pair but does not modify the store
or live pair. `apply` repeats all checks, holds the exclusive store lease,
creates the rollback generation, migrates and verifies the explicit store,
then activates both binaries.

After an interruption, use the same target-tag installer and binary directory:

~~~bash
go run github.com/ravinsharma7/missis/tools/paired-install@vX.Y.Z \
  --ref vX.Y.Z --bin-dir /absolute/bin --rollout inspect --json

go run github.com/ravinsharma7/missis/tools/paired-install@vX.Y.Z \
  --ref vX.Y.Z --bin-dir /absolute/bin --rollout recover --json
~~~

Inspection reports `discard-staged-old-generation`, `resume-migration`,
`finish-activation`, `finish-commit`, or a named integrity incident.
Recovery never searches for stores and never adopts an unrelated backup.

### Repository format-6 repair release

The proposed repair release is `v0.3.0`. It is a reviewed minor version
because the normal-open format, release-manifest schema, installation
procedure, and operator recovery behavior change together. It MUST NOT be
silently emitted as the next patch.

Before publication:

1. Commit the authoritative specification/registry changes, conformance tests,
   rollout implementation, release workflow, and operator documentation in
   reviewable commits. The final release commit MUST have no untracked or dirty
   source and MUST be the commit reviewed by CI.
2. Run `go test ./...`, the race suite used by the release workflow,
   `bash scripts/check-workflows.sh`, requirements coverage, generated
   onboarding verification, shell harnesses, `git diff --check`, and
   `check-done`.
3. Confirm the release commit reports format 6, normal-open format 6,
   migratable formats `[1,2,3,4,5,6]`, and the reviewed migration-set digest.
4. Dispatch `stable-release` with exact input `v0.3.0`. The workflow rejects
   an existing, older, prerelease, or invalid version. It builds both binaries
   from one full commit, lints workflows with pinned actionlint, verifies all
   archives/digests, creates a draft, exercises the pinned installer against
   draft assets, and only then publishes.

Before touching the authoritative installation, download the published
manifest and pair into a temporary binary directory. Verify the published
commit, both binary hashes, compatibility fields, a new disposable store, and
a disposable copy of the repository store. The copy MUST retain the same
store identity because it is read-only rehearsal input; it is never opened by
the old writer after being migrated.

Create and verify a fresh backup of the current authoritative format-6 store:

~~~bash
cd /absolute/path/to/project
go run ./tools/missis-tools backup \
  .missis-backups/pre-v0.3.0-install.db
go run ./tools/missis-tools backup verify \
  .missis-backups/pre-v0.3.0-install.db --against-current
~~~

The older pre-format-6 backup is retained migration evidence but is not the
rollback target: accepted events after that snapshot would be lost.

Plan and apply using the published target installer:

~~~bash
go run github.com/ravinsharma7/missis/tools/paired-install@v0.3.0 \
  --ref v0.3.0 \
  --bin-dir /absolute/path/to/bin \
  --rollout plan \
  --store /absolute/path/to/project/.missis-store/missis.db \
  --to-format 6 --json

go run github.com/ravinsharma7/missis/tools/paired-install@v0.3.0 \
  --ref v0.3.0 \
  --bin-dir /absolute/path/to/bin \
  --rollout apply \
  --store /absolute/path/to/project/.missis-store/missis.db \
  --to-format 6 --json
~~~

This particular store is already format 6, so apply MUST report no database
migration. It still verifies the store, captures the previous binary pair,
activates both target binaries, publishes the installation manifest last, and
verifies the installed maintenance tool. If interrupted, run the same
`v0.3.0` installer with `--rollout inspect`, then `--rollout recover`.

The previous `v0.2.2` pair is not a service-capable rollback generation for
this format-6 store. Recovery therefore finishes forward to the verified
`v0.3.0` pair. If the published pair itself is later found defective, stop
writers, preserve the incident files, verify/restore the fresh format-6 backup
to a copy, and use the reviewed source format-6 maintenance tool until a
corrected paired release is published. Do not restore the pre-format-6
database and do not put only one old binary back on PATH.

Close ticket #124 only after recording:

- the final release tag, full commit, and release-manifest digest;
- both installed binary digests and installation-manifest digest;
- rollout plan/apply receipts and any recovery journal disposition;
- fresh backup path/digest and `--against-current` result;
- explicit-store manifest and TUI smoke;
- disposable-copy interruption/recovery rehearsal;
- confirmation that bare `missis` and `missis-tools` resolve the installed
  `v0.3.0` pair and no old writer can open the authoritative store.

### Format-7 canonical-byte rollout

Format 7 is a new generation, not an in-place source-build convenience. Until
a reviewed stable pair advertising normal-open format 7 is published, the
authoritative repository store remains format 6 and must be opened only by its
installed format-6 pair. Do not migrate it with an uncommitted build.

The target release must rehearse this exact transition on the retained
revision-0006 fixture and on a disposable current-store copy:

~~~text
format 6 / global-json-chain-v1 head H, count N
  -> exclusive lease and verified pre-format7 backup
  -> apply 0012 without changing any historical event_json/hash/head
  -> format receipt, active epoch still global-json-chain-v1
  -> first new append writes exact bytes + canonical hash
     + integrity-epoch-transition-v1 in one transaction
  -> active epoch canonical-event-chain-v1
~~~

The rollout uses the published target tag and `--to-format 7`; its backup path
must not already exist. Verification must record old/new format, unchanged
store identity, source head/count/epoch, backup SHA-256, format receipt,
transition receipt after a disposable append rehearsal, exact accepted bytes,
and Linux/Windows compatibility results. Installation still follows the
journaled paired-binary algorithm above: stage pair, plan, quiesce, backup,
migrate, verify with staged pair, activate pair, write installation manifest
last. Before the migration commit, recovery returns to the complete format-6
generation. After it, recovery finishes forward with the verified format-7
pair or restores the bound backup and complete old pair; it never mixes them.

## What a missis store is

A single SQLite database file, holding:

- **The event ledger (source of truth):** `events` with
  `stream_kind`/`stream_entity`/`sequence`/`event_json`/`alias_seq`, plus the
  per-event hash chain in `event_hashes` and the chain head in `store_meta`.
- **Derived current projections:** `tickets` and `parts_current` — rebuildable
  current-time snapshots maintained transactionally on append (ticket #51).
- **Idempotency records:** `idempotency` mapping a client key to a versioned
  semantic request hash, the event IDs, and the JSON result it produced.
- **Migration bookkeeping:** `schema_migrations` recording applied versions.
- **Artifact metadata:** `artifacts` records durable content-addressed
  objects; blob bytes remain outside SQLite in the configured artifact root.

## Versioning and migration policy

- Schema migrations are **forward-only and additive**. Current set:
  `0001_init`, `0002_link_operation_index`, `0003_store_identity`,
  `0004_projection_snapshots`, `0005_artifacts`,
  `0006_ordered_parts`, `0007_store_format_revision`, and
  `0008_idempotency_request_hash`, `0009_store_identity_v1`, and
  `0010_external_ref_v1`, `0011_artifact_namespace_fork_v1`, and
  `0012_canonical_event_epoch_v1`.
- Store format is one internal integer independent of binary versions. The
  current value is 7. Stores through 0005 without a marker are implicit
  revision 1; unmarked stores through 0007 are implicit revision 2.
- New stores are created directly at revision 7. Revisions 1–6 are named
  inspection/migration inputs only. Normal open rejects them before WAL setup
  with the exact versioned command. Operators use `missis-tools store migrate
  plan --to-format 7` and `missis-tools store migrate apply --to-format 7
  --backup PATH`; the target format is mandatory.
- On open, the store: (1) probes compatibility read-only, (2) applies pending
  migrations in order, (3) verifies
  store identity and the hash chain, (4) backfills derived tables for stores
  created before migration 0004.
- Unknown migrations or a revision newer than the binary supports fail before
  WAL setup, migration, integrity verification, or projection repair.
- **Upgrades are explicit:** normal open never migrates an older format. The
  migration command requires and verifies a pre-migration backup before
  changing the source.
- **Writable-copy identity changes are separately versioned:** `missis-tools
  store fork plan --to-identity-version 1 --store COPY` reports the exact
  source identity and semantic artifact inventory. `fork apply` requires
  `--from-store-id ID` and `--backup PATH`; a non-empty artifact inventory also
  requires an absent `--destination-artifact-root` and resolves or accepts the
  exact source root. This operation changes identity, not store format.
  Plan resolves or accepts the source root read-only, fully hashes the CAS, and
  reports required object count, exact excluded refs, integrity issues,
  protocol, receipt version, and reconciliation blockers without creating a
  lock, child identity, backup, or staging path.
- A zero inventory uses `store-identity-fork-v1` and an empty child namespace.
  A reconciled non-zero inventory uses `artifact-namespace-fork-v1` plus
  `store-identity-fork-v2`. It copies the union of managed accepted references
  and artifact-index rows into an independent CAS, fully verifies source and
  child bytes, preserves unmanaged source identities as provenance without
  opening them, and lists but excludes valid CAS objects absent from both
  authoritative sets. An accepted managed ref without an index row blocks with
  an exact count and directs `artifacts rebuild-index-copy`; the fork never
  repairs the source in place. Hard links and shared writable namespaces are
  forbidden.
- The child identity is persisted before copying. The sorted manifest and its
  completion marker are flushed before atomic namespace publication; only then
  may SQLite atomically install the child identity, fork receipt, and indexed
  manifest/marker digests. `fork inspect` correlates disk and database state.
  `fork recover` resumes the same prepared identity and accepts an existing
  verified backup; ordinary `apply` rejects pre-existing backups and unrelated
  destination roots.
- **There is no downgrade path.** Once a store has been opened by a newer
  version, an older binary may not be able to read it. Forward-compatible with
  later versions; not backward-compatible with earlier binaries after
  migration.

### Compatibility corpus

The checked-in `internal/store/testdata/compatibility/revision-0007/` corpus
contains a deterministic database, manifest, and synthetic artifact CAS. It
covers every registered operation, built-in value/inline/reference kind,
relation, first-party ingestion plugin output, provenance shape, temporal
behavior, exact accepted bytes, integrity epochs, and derived projection
supported by revision 7. It uses fixed IDs,
UTC timestamps, logical slash paths, and synthetic bytes, so tests compare
logical state and hashes consistently on Linux and Windows.

Ordinary `go test ./...` regenerates the corpus in a temporary directory and
checks completeness and freshness. Compatible changes preserve revision 7
and its logical snapshot. Incompatible durable changes increment the revision
and add a retained fixture directory. Never rewrite an accepted fixture in
place. `go run ./tools/store-fixture --output DIR` is the explicit builder.

Revision 3 binds every active idempotency receipt to
`missis-request-v1:<sha256>`. Reusing a key for a different request is rejected
without appending. Format-revision-2 receipts have no recoverable original
caller request. Migration therefore moves them from the active `idempotency`
table into `idempotency_key_tombstones`; it never invents a request hash from
result or event data. A tombstoned key cannot be replayed or reused by guarded
mutation, so an old retry cannot appear to be a new request and append a
duplicate. Its original event IDs and result remain available through the
unguarded store-level audit lookup. The retained revision-2 fixture proves
that boundary.

Confirmed boundary: `v0.2.1` cannot verify revision-2 ledgers containing
ordered events because it did not deserialize `OrderKey` before recomputing
event hashes. Use the paired `v0.2.2` release or newer; this is not evidence of
corruption in such a store.

Revision 4 installs the exact `eventstore-hash-v1` identity document and
identity migration/fork receipts. Revision 5 preserves that identity, adds an
explicit format-migration receipt, and admits the strict `external-ref-v1`
durable value. External references reject unknown fields and never contain a
path, URL, credential, or embedded locator. Format 4 to 5 requires the exact
version-targeted migration and a pre-migration backup; it does not change
`store_id`.

Revision 6 preserves identity and accepted event interpretation while adding
the `artifact_namespace_forks` index and the versioned artifact fork
manifest/marker/receipt protocol. Format 5 to 6 requires the exact
version-targeted migration and a pre-migration backup; it does not change
`store_id`.

Revision 7 preserves every format-6 `event_json`, hash row, head, and store
identity while adding exact accepted-record bytes for new events. Historical
rows retain null codec/accepted-byte/content-digest fields and continue under
`global-json-chain-v1`; migration never manufactures canonical history. New
records use `canonical-event-chain-v1`, direct `sha256:` content identity, and
either `missis-event-canonical-json-v1` or `eventstore-record-json-v1` exact
bytes. The first post-migration append atomically binds the old head/count/
cursor and first new content/head in `integrity-epoch-transition-v1`.

## Integrity contract

- **Canonical event encoding v1** and its test vectors are defined by ticket
  #45. Format-7 events use persisted exact bytes as the live chain input;
  format-6 history retains its original verifier and hashes.
  The current scheme is named `global-json-chain-v1`: SHA-256 over the
  previous hex hash, a newline, and the implementation JSON event encoding.
  Ticket #57 owns migration to a new named integrity epoch; accepted history
  under `global-json-chain-v1` must never be silently rehashed.
- **Head hash** is the live SHA-256 chain described above. It is verified on
  every open (an intentional O(ledger) integrity check, ticket #51 decision)
  and by `show --health` / `CheckConsistency`.
- **Store identity** (`store_id`) is the domain-separated SHA-256 of exact
  `StoreIdentityDocumentV1` bytes containing a 32-byte OS-CSPRNG nonce. Migrated
  `missis-ulid-v1` stores receive a new ID and an atomic receipt binding the old
  ID, source head/count/epoch, new ID, and retained artifact namespace. A
  deliberately writable copy whose exact artifact/index inventory is zero
  receives another new document and a digest-addressed lineage receipt that
  preserves the exact parent document, fork head/count/epoch, backup digest,
  and new empty artifact namespace.
  Read-only copies keep the original identity. **Per-stream
  sequences** are unique and strictly increasing. A sequence gap is an integrity incident:
  accepted events are never rewritten, and recovery is restore-from-backup.
- **Bitemporal semantics** (ticket #42): the winner is
  `max(effective_at, recorded_at, stream_sequence, event_id)` among candidates
  with `recorded_at <= K` and `effective_at <= V`; boundaries are inclusive;
  retraction opens an interval hole; supersession voids as of known time.
- **Append batches are atomic, including across multiple streams** in one
  transaction (ticket #77): a failed batch writes nothing, and per-stream
  sequences are allocated independently.

### Artifact integrity and recovery

`missis-tools artifacts verify --json` takes offline exclusive leases,
semantically replays all accepted artifact references with exact event IDs and
field locations, and fully hashes every local CAS object. It distinguishes
unreferenced objects, missing/stale index rows, missing data/metadata, corrupt
bytes, and metadata conflicts. Equal size alone is never content verification;
normal artifact `Open` and deduplicating `Put` perform a full digest check.

GC protects the union of accepted semantic references and current index rows,
so a missing index cannot cause referenced bytes to be collected. If verified
bytes exist but the index is irreconcilable, `artifacts rebuild-index-copy`
creates and verifies a replacement database while leaving the source
untouched. Missing bytes require the exact digest from a verified backup or
trusted exact copy. The complete algorithm, result-to-recourse table,
injected-fault matrix, and not-yet-implemented artifact-namespace fork/remote
profiles are in `docs/artifact-integrity-and-recovery.md`.

## Live durability profile

- The confirmed writer configuration is SQLite WAL with
  `synchronous=NORMAL`. `show --health` reads and reports the active settings
  as `wal-normal`.
- This profile gives ordinary crash recovery and database consistency, but it
  does not promise that the newest acknowledged transaction survives host
  power loss. It is not a replication guarantee.
- The safety boundary, evidence states, and laptop-safe fault-injection ladder
  are in [`durability-testing.md`](durability-testing.md). Ticket #117 owns a
  configurable FULL profile and its benchmark; strict power-loss durability is
  currently **not confirmed**.

## Derived data

- `tickets`/`parts_current` are rebuildable from the ledger via
  `RebuildProjection` (SDK) or the automatic open-time backfill.
- `parts_current.order_key` is a rebuildable projection of the current
  containment event. Empty keys preserve the pre-order-key stream-sequence/Part-ID
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
- `missis-tools backup verify` distinguishes `complete`, the retained
  database-only backup state `legacy-v1`,
  `incomplete`, and `corrupt` backups. `backup cleanup` removes only stale
  staging paths and explicitly incomplete bundles; it never removes a valid
  published backup automatically.
- `missis-tools backup` writes a consistent copy while clients are active.
  Artifact migration and GC require an exclusive store maintenance lease and
  reject active clients.
- Published files are flushed before atomic rename. POSIX builds also sync the
  containing directory. Go does not support syncing directory handles on
  Windows, so Windows validates and closes the directory after flushing each
  file and relies on the platform's atomic replacement behavior; an
  unsupported directory `Sync` must not reject a verified publication.
- `missis-tools backup` accepts the existing database-only and version-1
  backup formats. Restoring a logical bundle verifies every referenced blob
  before publishing the new database and artifact root. Restore requires a
  new destination and exclusive leases for both destination resources.
- `missis-tools remote upload/download` uses content-addressed keys
  (`<store_id>/<head_hash>.db`), and download verifies store identity, head
  hash, schema version, and event count against a manifest computed from the
  live local store.
- `missis-tools backup verify [backup.db] --against-current` verifies a local
  backup against the live store. Omitting the path derives the current
  content-addressed backup under the project-aware backup directory.

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
  (with `os.UserConfigDir()` and the pre-user-config XDG fallback), and
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
- The pre-isolated artifact layout is `<store-directory>/artifacts/`. Run
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

The `version` JSON field and Git tag remain `v0.2.2`; `display_version` carries
the `+g<short-sha>` suffix. The suffix is SemVer build metadata and therefore
does not alter update precedence. Installer and self-update messages use the
display identity so operators can confirm the exact source commit.

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

### Removed maintenance executables

Only the paired `missis` and `missis-tools` executables are supported. Replace
the removed standalone wrappers as follows; no runtime aliases are provided.

| Removed executable | Replacement |
| --- | --- |
| `ticket-tui` | `missis-tools tui` |
| `repair-store` | `missis-tools repair` |
| `store-gaps` | `missis-tools gaps` |
| `store-manifest` | `missis-tools manifest` |
| `store-migrate` | `missis-tools store migrate plan/apply --to-format N` |
| writable-copy identity declaration | `missis-tools store fork plan/apply/recover --to-identity-version N` plus `store fork inspect` |
| `store-backup` | `missis-tools backup` |
| `store-remote` | `missis-tools remote` |

## Stability promise for revision 7

- The on-disk format is stable while `store_format_revision` remains 7.
- Any future breaking change to the ledger format is gated behind a new
  migration and documented here before release.
- Accepted `global-json-chain-v1` history will not be reinterpreted or silently
  rehashed; canonical adoption occurs only for new rows through the explicit
  receipt-bound epoch transition owned by #57.

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
