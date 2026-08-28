# Artifact integrity, recovery, and writable-fork procedure

**Implemented baseline:** Missis store format 6, 2026-08-28  
**Contract authority:** `specs/missues-issue-specification.v2.md`, sections 33
and 34. This runbook explains the implemented local adapter and artifact
namespace fork. Remote profiles remain proposals.

## 1. Terms and authority

An **accepted artifact-kind occurrence** is a value found by decoding a typed
artifact `Ref` field of an accepted event. It is not a string match. A
**managed CAS reference** has the exact
`artifact:sha256:<64 lowercase hex>` form. Existing accepted history also
contains explicitly classified **unmanaged non-CAS source identities** such as
`artifact:specs/report.md`; those retain provenance but make no claim that
Missis owns recoverable bytes. A malformed value beginning
`artifact:sha256:` is an error, not reclassified as unmanaged. The decoder
inventories `stream`, `target`, `sources`, `inputs`,
`causes`, effect references/evidence/before/after values, `Value.Ref`, typed
artifact descriptors, typed artifact-backed media, evidence and verification
values, and typed inline items. Every result carries the exact event ID and
field location.

The event ledger and immutable artifact bytes are authoritative. The SQLite
`artifacts` table and filesystem metadata sidecars are indexes/operational
metadata that can be checked and rebuilt. Replay can reconstruct the index;
it cannot reconstruct missing bytes.

Use this read-only check while all Missis clients are closed:

```text
missis-tools artifacts verify \
  --store /absolute/path/missis.db \
  --artifact-root /absolute/path/artifacts \
  --json
```

The verifier takes exclusive database and artifact-root leases, fully decodes
accepted events, fully hashes every CAS object, and prints each reference's
event IDs and field locations. Exit 0 means there is no accepted-data
integrity failure. `verified-with-unmanaged-references` names every preserved
non-CAS source identity and its event/field but does not assert bytes exist.
`verified-with-recoverable-staging` is also exit 0 because
the named temporary paths were never published or referenced; they still
require explicit cleanup. `inconsistent` is exit 1.

## 2. Local publication algorithm

The implemented `LocalStore.Put` algorithm is:

1. Create a mode-0600 temporary data file under the selected artifact root.
2. Copy the supplied stream once while computing SHA-256 and byte count.
3. Flush the file, close it, and derive the canonical reference from the
   computed digest.
4. Create the digest's fixed CAS parent directories.
5. If the data path is absent, rename the temporary file to the digest path
   and sync its parent directory. A partially copied temporary file is never a
   CAS object.
6. If the data path already exists, require the same size and fully hash the
   existing bytes. Same-size content is not assumed equal.
7. Write the metadata sidecar through a temporary mode-0600 file, flush it,
   rename it, and sync its parent directory.
8. Fully verify the published data against the reference, metadata digest,
   and size before returning metadata to the application.
9. Ingestion appends the artifact index row and the events that refer to it in
   one SQLite transaction. If that transaction fails, the CAS object is a
   safe unreferenced object; no accepted event claims it.
10. Return `artifact:sha256:<digest>` only after these steps succeed.

There is no algorithm step that copies a caller-owned pathname into an event.
The caller's file can be changed, moved, or deleted after `Put` because Missis
owns a verified byte copy. Paths and filenames are descriptive provenance,
not content identity.

### 2.1 Read and verification algorithms

These operations have deliberately different costs:

| Operation | What it checks | Guarantee and limit |
| --- | --- | --- |
| `Stat` | Reference syntax, metadata sidecar, algorithm, digest field, and current file size. | Cheap metadata observation. It does **not** hash bytes and therefore cannot detect a same-size edit. Callers must not treat `Stat` as content verification. |
| `Open` | Calls full `Verify` before opening the data file. | A same-size edit present before the call is rejected. Missis does not claim protection from a hostile same-user process modifying the file concurrently after verification; filesystem permissions and exclusive maintenance leases address ordinary operation, not a malicious local principal. |
| `Put` deduplication | Fully hashes an existing same-reference object. | A corrupted same-size object is rejected, not silently reused. |
| `artifacts verify` / CAS scan | Fully hashes all objects and compares accepted-event references, index rows, metadata, and paths. | Complete local artifact audit for the named root at the leased snapshot. |

The stronger `Open` behavior is intentional for alpha correctness. A later
verified-object cache may reduce hashing only if invalidation is tied to a
filesystem/object version proof; modification time and size alone are not a
proof.

## 3. Exact verifier states and recourse

| Reported state/issue | Confirmed meaning | Safe recourse |
| --- | --- | --- |
| `healthy` | Accepted reference, index row, metadata, and fully hashed bytes agree. | None. |
| `unmanaged-reference` | Accepted history contains a named artifact-kind source identity that is not a managed SHA-256 CAS reference. Exact event/field provenance is known; managed bytes are neither asserted nor guessed. | Preserve it. If durable bytes are required, explicitly re-ingest a trusted source as a new CAS artifact and record the relationship; do not rewrite accepted history. |
| `unreferenced-object` | Valid CAS bytes have neither an accepted typed reference nor an index row. | Retain, or run dry-run GC with an explicit grace period and then confirm deletion. |
| `missing-index` | Named accepted events/fields reference valid or separately classified bytes, but the derived SQLite row is absent. | Build and verify a replacement database with `rebuild-index-copy`; do not edit accepted events. |
| `indexed-without-accepted-reference` | An index row exists but typed event replay found no accepted reference. | Preserve it during ordinary GC. Inspect imports/old codecs; rebuild a replacement index only after the replay inventory is accepted. |
| `missing-object` | Accepted history or the index names a digest whose data file is absent. | Stop serving the object. Restore that exact digest from a verified backup or trusted exact replica, then rerun verification. Replay cannot create the bytes. |
| `missing-metadata` | Data exists but its sidecar is absent. | Fully hash the data. If its digest equals the path/reference, reconstruct metadata in a replacement/recovery operation; otherwise treat it as corrupt. |
| `corrupt-object` | The computed digest or size differs from the expected values. | Quarantine the changed bytes for diagnosis; restore the expected digest from a verified copy. Never rename altered bytes to the expected digest. |
| `index-object-metadata-mismatch` | The derived row conflicts with the verified object metadata. | Rebuild the index in a new database; retain the source and comparison report. |
| invalid CAS path/symlink | Files exist outside the fixed digest layout or a symlink enters the CAS. | Do not follow or serve it. Quarantine after inspection; never infer a reference from the path. |

Every `missing-object` and `corrupt-object` report includes the reference; a
typed accepted reference also includes all known event IDs and field
locations. `corrupt-object` includes expected and computed digest/size. This
is the operator's exact restore list; there is no guessed “probably live” set.

After restoration, run `artifacts verify` again. Repair is complete only when
the report is `verified` (or only separately acknowledged recoverable staging
remains). A restored object is never accepted merely because its filename or
size matches.

## 4. Rebuilding a conflicting index without touching the source

When accepted event replay, index rows, and CAS metadata disagree but all
accepted object bytes are present and valid, create a replacement candidate:

```text
missis-tools artifacts rebuild-index-copy \
  --store /absolute/path/source.db \
  --artifact-root /absolute/path/artifacts \
  --destination /absolute/path/rebuilt.db \
  --json
```

The command:

1. requires an absent destination distinct from the source;
2. takes exclusive database and artifact leases;
3. decodes all accepted events and records exact event/field occurrences,
   separating managed CAS references from named non-CAS sources;
4. fully verifies every managed referenced object;
5. creates a SQLite backup at a same-directory staging path, preserving exact
   accepted events, identity, head, and receipts;
6. replaces only the destination's `artifacts` index in one transaction;
7. runs store consistency verification on the destination;
8. atomically renames the completed database to the requested destination;
9. reports store ID, head digest, event count, accepted-reference count,
   source index count, managed reference/rebuilt counts, and unmanaged source
   count.

The source database is unchanged. The output is an exact-identity replacement
candidate, not a second writable authority. Verify it, stop all users of the
source, quarantine the source database, publish the rebuilt database at the
original authority location, and retain the source plus JSON reports until
the recovery retention decision. If object verification fails, no destination
is published; restore the named bytes first.

## 5. Restart and injected-fault matrix

| Durable observation after restart | Classification | Detection | Recovery |
| --- | --- | --- | --- |
| `.artifact-*tmp` or `.metadata-*tmp` only | Interrupted unpublished staging. | `artifacts verify` reports `staging_paths`; `artifacts gc --grace D --dry-run` reports `stale_temp`. | After the explicit grace period, rerun GC with `--confirm`. |
| Complete valid CAS object, no accepted reference/index | Safe unreferenced object. | `unreferenced-object`. | Reuse by digest on a later `Put`, or explicit grace-period GC. |
| Accepted reference, no index | Rebuildable derived-index loss. | `missing-index` plus event IDs/locations. | `rebuild-index-copy`, verify, then controlled replacement. |
| Index row, no accepted reference | Stale/unproven index metadata. | `indexed-without-accepted-reference`. | Preserve; inspect or rebuild a replacement index. |
| Accepted reference, absent/changed bytes | Integrity incident. | `missing-object` or `corrupt-object`, expected/computed identity. | Fail stop for that object; restore exact bytes and reverify. |
| Client lost the SQLite response | Append outcome may be committed or rolled back. | Query the versioned idempotency receipt/event IDs using the original key and request hash. | Replay the identical request only; a different request is `idempotency_mismatch`. |

Hermetic tests inject and confirm: interrupted staging discovery and explicit
cleanup, unreferenced-object classification, missing-index reconstruction in a
new database, GC preservation when the index is missing, same-size byte
tampering on `Open` and maintenance verification; writable-fork tests cover
independent indexed-object copying, unmanaged provenance, unreconciled
accepted references, publication-before-commit recovery, and exact inspection.
Host power-loss evidence remains separate and uses the
disposable-VM procedure in `docs/durability-testing.md`.

## 6. Writable fork with an independent artifact namespace (implemented)

A source store enters this procedure when its exact fork plan reports any
non-zero value for:

- `artifact_record_count`: SQLite artifact-index rows, including rows not
  proven reachable from accepted history;
- `managed_cas_reference_occurrences`: typed accepted-event fields naming
  `artifact:sha256:<digest>` objects;
- `unmanaged_source_reference_occurrences`: typed accepted-event fields naming
  artifact-kind provenance without a managed CAS digest.

`accepted_artifact_reference_event_count` separately reports how many accepted
events contain one or more of the latter two occurrence classes. It is useful
for audit scope but is not a substitute for the occurrence counts. Zero in all
three classes uses an empty child namespace and `store-identity-fork-v1`. Any
non-zero class selects `artifact-namespace-fork-v1` and
`store-identity-fork-v2`; the planner does not guess from filenames or JSON
substrings.

An ordinary backup/read-only replica preserves `store_id` and namespace. A
deliberately writable child receives a new identity and independent bytes:

```text
missis-tools store fork apply \
  --store /absolute/path/child.db \
  --to-identity-version 1 \
  --from-store-id store:v1:sha256:<parent> \
  --backup /absolute/path/pre-fork.db \
  --source-artifact-root /absolute/path/parent-artifacts \
  --destination-artifact-root /absolute/path/child-artifacts
```

The source root defaults from the current identity when managed bytes exist.
The destination is explicit, different, and absent, except when `recover`
matches an already prepared namespace. Paths are operational configuration and
never enter accepted events, identities, manifests, or receipts.

The algorithm is:

1. take exclusive database, source-root, and destination-root leases in a
   deterministic order;
2. verify the ledger and derive the required object set as the union of typed
   managed accepted references and all artifact-index rows. If any accepted
   managed ref lacks an index row, stop with the exact count and require
   `artifacts rebuild-index-copy`; do not repair the source in place;
3. preserve unmanaged values and exact event/field locations as
   `provenance-only-unmanaged-v1`; never open them as paths or locators;
4. create, hash, and verify the required source-bound SQLite backup. Recovery
   may reuse only the explicitly named existing non-empty backup after its
   format, identity, head, event count, and ledger chain match the plan;
5. generate one child identity and flush it in
   `artifact-namespace-fork-operation-v1.json` under the deterministic sibling
   staging root. Retry validates and reuses it;
6. fully verify every required source object and its index metadata. Missing,
   corrupt, or mismatched data returns `artifact-integrity-failure` with the
   exact ref before identity or accepted-ledger mutation; the verified backup
   and incomplete staging remain visible recovery evidence;
7. stream-copy each object into the child CAS without hard links, then verify
   the child's digest, size, algorithm, and sidecar;
8. scan the source CAS. Valid objects in neither required set are listed under
   `excluded-unreferenced-v1` and not copied; any invalid CAS entry blocks the
   fork;
9. write and flush sorted canonical
   `artifact-namespace-fork-manifest-v1.json`, binding source head/count,
   parent/child IDs, copied entries and counts, unmanaged inventory, and exact
   excluded refs;
10. write and flush `artifact-namespace-fork-complete-v1.json` last, binding the
   manifest digest, then atomically rename staging to the destination and sync
   the parent directory;
11. atomically install the child identity, `store-identity-fork-v2` receipt,
    and `artifact_namespace_forks` row binding manifest/marker digests, counts,
    dispositions, backup digest, parent head/count, and integrity epoch;
12. correlate and verify database identity, receipt index, marker, manifest,
    and every object before reporting completion.

The source namespace is never deleted by this operation. Per-store physical
copies are the alpha default so retention, GC, backup, restore, and corruption
are isolated.

### 6.1 Fork restart states

| Marker/receipt state | Meaning | Required action |
| --- | --- | --- |
| Operation record only, or manifest without marker | Copy was never publishable. | `inspect` reports `copy-incomplete` or `manifest-written-copy-incomplete`; `recover` re-verifies/reuses objects and the same child identity. An operation-less staging root is never adopted or deleted automatically. |
| Marker and manifest, parent database identity | Artifact copy is prepared but the identity transaction is uncommitted. | `inspect` reports `prepared-awaiting-database-commit`; run the exact versioned `recover` with the same parent ID, roots, and backup. |
| Receipt plus matching marker/manifest/namespace | Complete fork. | Verify once more and allow the child authority to open. |
| Receipt but namespace/marker/manifest missing or mismatched | Database claims a fork whose bytes are unproven. | Integrity incident: fail stop the child, restore its pre-fork database, retain evidence. |

`missis-tools store fork inspect --store CHILD.db
--destination-artifact-root CHILD-ARTIFACTS --json` creates nothing. It fully
hashes manifest objects and returns `absent`, `incomplete-without-operation`,
`copy-incomplete`, `manifest-written-copy-incomplete`,
`prepared-awaiting-namespace-publication`,
`prepared-awaiting-database-commit`, `complete`, `identity-mismatch`, or
`integrity-failure`. `complete` requires the database receipt index to match
the exact manifest and completion-marker bytes.

## 7. Deduplication, media type, retention, and remote backends

Two references with the same SHA-256 name the same bytes, even if their domain
meaning or declared media type differs. The recommended v3 split is:

- global object metadata: algorithm, digest, size, backend object version;
- per-reference metadata: media type, source name, role, consumer schema, and
  provenance;
- per-store reachability: accepted event IDs/locations and retention policy.

This permits byte deduplication without asserting that identical bytes have
one semantic interpretation. The current local index still stores one media
type per digest and rejects conflicting non-empty types; moving media type to
per-reference metadata is therefore an explicit later format/schema change,
not silently implemented in format 6.

A machine-global CAS can save space, but it is unsafe until GC takes a lease
and computes reachability across **every** owning store/namespace. One store
must not delete bytes still live in another, and one store's retention or
authorization must not reveal another's evidence. Alpha therefore copies
objects per store. A later shared CAS should be a service with per-store
leases/refcounts, authorization, backup responsibilities, and auditable GC—not
an uncoordinated shared directory or hard links.

Remote object storage implements the same abstract publication guarantee but
not the filesystem rename algorithm. Its backend profile must be versioned
separately and use an operation ID/staging key, client-computed digest, complete
multipart upload, verified checksum or read-back, conditional creation of the
final digest key, and a completion manifest/marker written last. Ledger
acceptance occurs only after the final object is readable and verified under
the profile. Recovery uses operation receipts and markers; it must not depend
on eventually consistent bucket listing.

Remote conformance must inject at least: interrupted multipart upload,
successful object with missing marker, marker with missing/wrong object,
conditional-create race, stale `HEAD`, delayed read visibility, provider
checksum mismatch, object-version replacement, expired credentials, lifecycle
deletion, encryption-key loss, cross-region replica lag, and retry after an
ambiguous timeout. Provider versioning/object lock can strengthen recovery but
does not replace Missis digest verification. Remote publication can therefore
be a separate adapter/profile, while its accepted-reference and receipt
semantics remain shared.
