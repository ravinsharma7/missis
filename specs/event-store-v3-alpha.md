# Event-store v3-alpha.4 contract and extraction boundary

**Status:** authoritative v3-alpha event-store contract
**Owner:** ticket `#118`
**Protocol version:** `eventstore-v3-alpha.4`
**Contract bundle digest:** unassigned; manifest tooling is not implemented
**Evidence baseline:** `reports/missis-event-store.md`, current implementation,
and retained store fixtures as of 2026-08-27

This file defines the active **v3-alpha** event-store contract. It supersedes
the frozen v2 document only for event-store protocol, persistence, integrity,
store identity, external-reference, artifact-storage, and extraction work.
`missues-issue-specification.v2.md` remains the authoritative frozen Missis
product/domain contract; `phase1-requirements.md`, the requirements registry,
and live tickets remain authoritative in their own scopes.

The alpha label is deliberate: Missis, Spy Testing, and CSS Flight Recorder
must prove the same neutral conformance contract before this becomes a stable
protocol or reusable package.

## 1. Version namespaces

These versions are independent and MUST NOT be compared as if they were one
number:

| Version | Meaning | Current/proposed state |
| --- | --- | --- |
| Missis product specification v2 | Ticket/Part/ontology/CLI behavior | Frozen authoritative domain contract; only correctness/security/data-loss errata may change it. |
| Event-store protocol v3-alpha.N | Consumer-neutral record, receipt, integrity, projection-hook, and reference contract | Active authoritative alpha contract; `N` increments for incompatible alpha contract changes. |
| SQLite store format revision 7 | Physical Missis database compatibility boundary | Adds exact accepted-record bytes, codec/content identity, event integrity epochs, and a receipt-bound transition to revision 6's artifact/reference/identity foundation. |
| Canonical event codec v1 | Exact accepted event-byte encoding | Defined by `#45`; live for newly accepted format-7 records. Format-6 history retains its original verifier. |
| Idempotency request fingerprint v1 | Domain-separated request/operation fingerprint | Implemented by `#116`. |
| Store identity scheme | Immutable authority identity, independent of location and mutable content | Revision 4 implements `eventstore-hash-v1`; older `missis-ulid-v1` stores are explicit migration inputs and receive an identity receipt. |
| Integrity scheme/epoch | How accepted bytes are chained or checkpointed | Format-6 history remains `global-json-chain-v1`; new format-7 records use `canonical-event-chain-v1`, with the first mixed-epoch record bound by `integrity-epoch-transition-v1`. |
| Contract bundle digest | Digest of the exact protocol schemas, canonical vectors, and requirements used by one build/store | Proposed below; independent of physical format and product version. |
| Projection version | Consumer-specific reducer/index interpretation | Must be declared independently per projection. |

Opening a format-revision-3 database does not mean that event-store v3-alpha
is implemented. Conversely, another adapter may implement the v3-alpha
protocol without using SQLite format revision 7.

### 1.1 Alpha subversions and contract identity

`v3-alpha` is a maturity family, not a durable wire identifier. Every testable
alpha build MUST publish a concrete protocol identifier such as
`eventstore-v3-alpha.3`. Increment the final integer whenever an accepted
record envelope, receipt, reference, integrity proof, capability report, or
required conformance result changes incompatibly. Compatible editorial
changes do not increment it.

Each build also publishes a canonical `ContractBundleV1` manifest:

```text
protocol_version
record_codec_ids[]
request_fingerprint_ids[]
integrity_scheme_ids[]
reference_codec_ids[]
receipt_codec_ids[]
capability_schema_id
projection_contracts[]       consumer/schema/version/digest
conformance_vector_digests[]
component_digests[]           repository-relative logical name + SHA-256
source_revision?              provenance only; not the contract identity
```

The bundle digest is domain-separated SHA-256 over the canonical manifest with
the digest field omitted:

```text
SHA256("MISSIS-EVENTSTORE-CONTRACT" || 0x00 || "v1" || 0x00 || manifest_bytes)
```

The full digest, not a short prefix or Git commit, is the contract identity.
A short prefix MAY appear in developer build labels, but receipts, health,
backups, fixtures, and compatibility errors use the full digest. A Git commit
records source provenance; it does not replace the digest because one commit
may contain multiple consumer schemas and generated artifacts. Physical store
revision, consumer projection versions, and contract bundle digest remain
separate fields so a migration in one namespace does not pretend to upgrade
the others.

### 1.2 Maturity gates: alpha to stable v3

| Stage | Required evidence and freeze |
| --- | --- |
| `v3-alpha.N` | Design may change incompatibly. Every build is self-identifying by concrete alpha number and contract bundle digest. Disposable data or explicit migrations are acceptable. |
| `v3-beta.1` | Neutral envelope, exact-byte rules, receipt, reference codec, startup modes, integrity transition, and capability schema are frozen; Missis, Spy Testing, and CSS Flight Recorder pass the same conformance corpus in separate stores; at least two language implementations pass canonical vectors; no unowned compatibility path remains. |
| `v3-rc.N` | Migration and rollback rehearsals pass on retained fixtures and copies of real stores; backup/restore, crash, durability, corruption, stale-reference, fork, and security suites pass; all three consumers complete a pilot without protocol exceptions; performance budgets and support range are published. |
| `v3` | The exact contract bundle is accepted into a new authoritative specification and requirements registry, release-tagged, and immutable. Supported migrations, deprecation periods, operator runbooks, compatibility matrix, and failure classifications are published; no open P0/P1 protocol correctness work remains. |

Freezing v2 therefore does not promote this draft. Until every stable gate is
confirmed, the status remains alpha (or beta/RC when its row is fully met),
and unknown evidence is reported as unknown rather than waived.

### 1.3 Compatibility retirement policy

New v3 identifiers, errors, schema fields, and documents MUST NOT use the
generic word “legacy.” Every older shape is named by format, codec, integrity
scheme, layout, or decision epoch and receives one disposition:

| Existing class | Current evidence | Alpha disposition |
| --- | --- | --- |
| Format-v2 unbound idempotency receipts | Confirmed in retained fixture; 178 repository-store rows were migrated to permanent tombstones when that store advanced to revision 3. | Format revision 2 is explicit migration/inspection input only: never a normal write format and never an origin of v3 external references. Retain the fixture and tombstone audit evidence. |
| `global-json-chain-v1` accepted hashes | Confirmed active for all current ledgers. | Retain immutable verification; transition only through a named integrity epoch and receipt (`#57`). |
| Database-only and backup-bundle-v1 inputs | Supported readers exist; external usage is not yet inventoried. | Measure retained backups, publish an import/support deadline, then remove only with recovery evidence. |
| Pre-isolated artifact root | Quarantined old-layout roots may still exist after migration. | Keep reversible migration and explicit GC; never delete operator data merely because alpha starts. |
| `.missis.d` context/pointer metadata | Non-authoritative onboarding input, not store identity or task direction. | Inventory readers and migrate useful settings to explicit configuration before removal. |
| Pre-order-key containment and pre-evidence retraction events | Immutable accepted history can require old reducer semantics. | Retain versioned decoding/folding or produce an explicit semantics-preserving migration receipt; do not silently reinterpret. |
| Public/internal fields named `Legacy*` | Usage and API exposure are not yet fully classified. | Rename or remove only after call-site/API inventory and a migration note. |

The v3 stable gate is not “zero old data.” It is zero unnamed or unowned
compatibility behavior: every retained reader has fixtures and a support
reason; every removed path has migration/recovery evidence.

### 1.4 Store identity and format-v2 retirement

Store identity is not a repository path, machine identifier, project name, or
digest of mutable ledger content. The v3 target derives the full 256-bit
`store:v1:sha256:<digest>` identifier from canonical immutable identity-document
bytes and a 32-byte cryptographically random nonce. Appends change head state,
not identity. Read-only replicas and backups preserve the identity document;
writable forks receive a new identity and explicit lineage.

Every peer claim includes both immutable identity/genesis evidence and mutable
head/count evidence. Same ID with incompatible immutable evidence is an
identity collision. Same identity with different heads is replica divergence
until ancestry/checkpoint evidence proves a stale/advanced relationship. A
resolver selects neither case by path order. Exact canonical mechanics and
conformance cases are owned by `cross-store-references-v3-alpha.md` and
ticket `#122`.

### 1.5 Paired-release and store-format rollout

A paired binary update and a store-format migration are separate durable
transactions. They MUST NOT be described as one atomic filesystem operation.
When one release changes the normal-open store format, they MUST instead be
coordinated as one versioned, crash-inspectable rollout with exactly two valid
terminal generations:

~~~text
old generation = old paired binaries + old installation manifest
               + old store/artifact generation

new generation = new paired binaries + new installation manifest
               + migrated store/artifact generation + migration receipt
~~~

A mixed generation is never a supported operating state. The old writer MUST
NOT write the migrated store, a new normal client MUST NOT write the
source-format store, and one binary from each release MUST NOT be treated as a
pair.

Every release manifest that can participate in a rollout MUST bind:

~~~text
normal_open_format
migratable_from_formats[]     # sorted, unique, exact physical revisions
migration_set_digest          # full SHA-256 over the migration catalog
~~~

`store_format_revision` remains a compatibility spelling of
`normal_open_format` during alpha and the two values MUST agree. Merely
naming the target format does not prove that the staged maintenance tool
understands a particular source revision.

The migration-set digest is:

~~~text
SHA256(
  "MISSIS-STORE-MIGRATION-SET" || 0x00 || "v1" || 0x00 ||
  for each embedded migration sorted by filename:
    filename || 0x00 || decimal_byte_length || 0x00 ||
    exact_file_bytes || 0x00
)
~~~

Directories are excluded. Filenames and bytes are taken from the compiled
migration catalog, not the working directory. Adding, removing, renaming, or
changing a migration therefore changes the digest.

The rollout state machine is:

~~~text
inspect -> stage-pair -> plan -> quiesce -> backup -> migrate
        -> verify-staged -> activate-pair -> committed
~~~

1. **Inspect** reads the current installation and store generation without
   mutation and records binary digests, store identity, format, head, event
   count, integrity epoch, artifact namespace, and outstanding rollout state.
2. **Stage-pair** obtains both target binaries in a non-live generation and
   verifies release, archive, and binary digests, pair identity, normal-open
   format, source-format membership, and migration-set digest before executing
   staged code.
3. **Plan** invokes the staged maintenance implementation by absolute path. It
   names the exact source/target revisions, store and artifact paths, backup
   destinations and space, identity effects, maintenance requirement, and
   rollback generation. Plan is read-only.
4. **Quiesce** stops project clients and acquires the exclusive store lease.
   Inspect and plan are repeated under that lease so no stale plan is applied.
5. **Backup** creates and verifies the pre-migration database/artifact bundle
   and preserves the previous paired binaries and installation manifest. A
   rollout journal binds all digests and is durably published before mutation.
6. **Migrate** applies only the exact requested format transition. Its receipt
   makes repetition return the completed result instead of applying twice.
7. **Verify-staged** uses only the staged pair to check format, identity and
   migration receipts, head/count, required integrity/projection checks, and
   artifact namespace/manifest. While the exclusive lease is held, the target
   installer executes the same target-version verification implementation
   in-process because a second process cannot acquire that lease. Immediately
   after paired activation and lease release, the installed maintenance binary
   MUST repeat an explicit-path non-mutating smoke before commit.
8. **Activate-pair** replaces both live binaries as one journaled generation
   and writes the installation manifest last. PATH discovery is not evidence
   of activation.
9. **Committed** is recorded only after the installed pair and migrated store
   are re-inspected together.

On restart, pre-migration states discard staging without changing the old
generation. A store migrated before binary activation either completes
activation from the still-verified staged generation or restores the bound
store/artifact backup; it never guesses from schema filenames or PATH.
Activation-complete states verify and finish the journal. Any digest, identity,
head, receipt, or namespace disagreement is an integrity incident requiring
explicit operator recovery.

Installing a global binary pair MUST NOT discover and migrate arbitrary
stores. Each store is an explicit rollout target. Multiple stores may remain
on different supported generations, and inspection MUST name which stores
still require a rollout. Development builds may exercise the protocol
hermetically, but MUST NOT create a stable installation manifest or be
represented as the permanent repair for an authoritative store.

Confirmed implementation state: newly created stores are physical revision 7.
Normal open accepts revision 7 only; revisions 1–6 remain read-only inspection
and explicit version-targeted migration inputs. The migration command requires
a pre-migration backup, preserves old artifact reachability through an explicit
artifact namespace, and installs exact identity bytes plus an old/new receipt
atomically. Retained older fixtures remain required recovery evidence.

A separate identity-version operation handles deliberate writable copies:
`store fork plan/apply/recover --to-identity-version 1`. Apply requires the
expected parent `store_id` and a rollback backup. A zero artifact inventory
uses `store-identity-fork-v1` and a new empty namespace. Any index row, managed
CAS occurrence, or unmanaged artifact-kind source occurrence selects
`artifact-namespace-fork-v1` and `store-identity-fork-v2`: managed/indexed
objects are fully verified and copied (never hard-linked) after every accepted
managed reference has a matching index row; a missing row requires the
replacement-copy replay workflow. Unmanaged values
remain exact provenance and are never opened as paths, and valid CAS objects
in neither authoritative set are listed but excluded. The operation persists
the prepared child identity, sorted manifest, and completion marker before
publishing the namespace, then commits the identity and receipt atomically.
`store fork inspect` distinguishes incomplete copy, prepared/uncommitted,
complete, identity mismatch, and receipt/namespace integrity failure. Recovery
reuses the prepared identity and verified objects; it never generates a second
child for the same staging state. Physical paths remain operator configuration
and are absent from accepted identity and receipt bytes.

`store fork plan` is read-only. When managed/indexed objects exist it resolves
or accepts the source root, fully scans the CAS, and reports required object
count, exact valid excluded refs, integrity issues, missing-index count,
protocol, and receipt version. It creates no lease file, child identity,
backup, destination, or staging state.

## 2. Alpha boundary and consumers

### 2.1 Neutral kernel

The reusable kernel may understand only these concepts:

```text
store identity and authority
namespace
stream identity and stream revision
atomic accepted batch / commit receipt
versioned canonical record bytes
request-bound idempotency
typed references and lineage
opaque change-feed cursor
integrity epoch and checkpoint
durability profile
artifact descriptors
projection version/watermark hooks
compatibility and conformance fixtures
```

It MUST NOT assign built-in meaning to `ticket`, `part`, `project`, `group`,
Markdown, ontology, CSS selector, browser target, test run, probe, snapshot, or
golden. These are consumer schemas and projections.

### 2.2 Consumer adapters

| Consumer | Owns | Must not leak into the kernel |
| --- | --- | --- |
| Missis | Ticket/Part/Link operations, bitemporal winner rules, project/group membership, ontology, Markdown transport, human aliases | `ticket_id`, part paths, `#184`, `has-home`, done-when rules |
| Spy Testing | Runs, source capsules, recorder segments, observations, probe plans, snapshots, baselines, replay, decisions, containment | test framework names, workspace semantics, probe/replay rules |
| CSS Flight Recorder | Browser sessions, targets, stylesheet revisions, traces, snapshots, cascade candidates, state graph, coverage | DOM/CDP types, selector/cascade semantics, browser indexes |

Each adapter validates its schema and produces its own projection deltas. The
kernel provides atomic storage and invokes versioned hooks; it does not decide
the consumer's domain truth.

### 2.3 First alpha deployment shape

The hill-climbing default is one physical store and artifact root per tool,
with one local authority per store. Sharing a contract does not require sharing
a database. A common conformance suite comes before a common implementation,
and a common implementation comes before any shared physical database.

The first non-Go boundary SHOULD be a versioned JSON protocol over a local
collector/CLI/service. Direct SQLite access is an adapter format, not the
portable protocol.

### 2.4 Neutral accepted-record envelope

The proposed logical envelope is:

```text
protocol_version
namespace
record_id
schema_id
schema_version
stream_id
stream_revision             authority assigned
batch_id
recorded_at                 authority assigned
effective_at?               consumer-defined optional meaning
actor?
record_codec
payload_codec
payload_bytes               exact consumer payload under payload_codec
artifact_refs[]
lineage_refs[]
content_hash                hash of canonical accepted record bytes
integrity_epoch
```

The kernel treats `schema_id`, payload bytes, and typed references as opaque
until the selected consumer adapter validates them. `effective_at` is optional
at kernel level: Missis requires bitemporal interpretation, while CSS and Spy
may use recorded time, monotonic offsets, causal order, or domain timestamps.
`record_codec` canonicalizes the complete accepted envelope after authority
fields are assigned, excluding the content-hash and chain/checkpoint outputs
that would otherwise be circular. Those exact canonical record bytes are what
V3-BYTES-001 requires the adapter to preserve.

`eventstore-record-json-v1` is the first exact neutral record codec. Its JSON
object fields occur in this fixed order:

```text
protocol_version, namespace, record_id, schema_id, schema_version,
stream, stream_revision, batch_id, subject, recorded_at, effective_at, actor,
record_codec, payload_codec, payload_bytes
```

`stream` and `subject` are fixed `{kind,id}` objects. `batch_id` is the empty
string for a single unbatched append or the authority-assigned identifier
shared by every record in one accepted multi-record batch. Timestamps are UTC with
exactly nine fractional digits. `payload_bytes` uses JSON's canonical base64
encoding for a byte string; it is not reparsed or normalized as JSON merely
because `payload_codec` names JSON. HTML characters are not escaped. Duplicate
or unknown fields are invalid for the v1 decoder; the stored bytes nevertheless
remain available so an unknown later codec is classified as unsupported, not
corrupt. `content_hash` is `sha256:<lowercase hex>` over these exact bytes and
is outside the encoded object to avoid circular input.

## 3. Decision and compatibility map

| Area | Classification | Proposed v3 decision | Public behavior | Owner |
| --- | --- | --- | --- | --- |
| Idempotency key reuse | Strengthen and fix | Bind the key to a versioned request fingerprint; mismatch is a conflict with no append. | Intentional correction: a different request no longer silently replays the first result. Same-request replay is preserved. | `#116` |
| Format-v2 idempotency rows | Clarify migration | Never infer a caller request from result/events. Move the row to a permanent key tombstone; guarded replay/reuse fails closed and audit lookup remains. | Old keys require replacement after upgrade; this prevents duplicate execution without blessing a guessed request. | `#116` |
| Live event hashing | Clarify current state, then change by epoch | Preserve the internally consistent `global-json-chain-v1` epoch. Introduce canonical-v1 only through a versioned hash epoch and migration receipt. | No accepted event or existing hash is rewritten. New verifier/receipt fields are additive. | `#45` done; `#57` open |
| Exact accepted bytes | Strengthen | Persist or otherwise preserve the exact versioned canonical accepted bytes. Column type is adapter-specific. | No domain API break. | `#57` |
| Durability | Clarify and strengthen disclosure | Every adapter reports its active durability profile. Current local SQLite is WAL/NORMAL; newest acknowledged commits are not promised across host power loss. | Health gains fields. A future strict profile is opt-in until benchmarked. | `#117` |
| Hot-stream append | Preserve behavior, rewrite internals | Bound work by affected logical keys/history and intentionally affected descendants; use projection deltas and indexed current links. | No intended CLI/SDK result change. | `#119` |
| Ordered projection startup work | Preserve behavior, rewrite internals | Use trustworthy projection versions/watermarks and affected-stream repair. | No intended query-result change. | `#109` |
| Full hash verification at startup | Deferred intentional behavior change | Keep fail-fast verification for v2. A checkpoint-plus-tail startup may replace it only with an explicit threat model, trusted checkpoint, and deep verifier. | If accepted later, old corruption moves from every-open detection to explicit/deep-audit detection. | `#51` decision; `#58`, `#110` prerequisites |
| Local/server authority and sync | Deferred | Do not turn report architecture into contract until the authoritative storage model is decided. | Unknown. | `#48` |
| Cross-store work references | Strengthen identity model | Foreign identity is `store_id` plus canonical entity/event identity; aliases, paths, labels, and locators are hints. | Adds structured references and unresolved-foreign states; does not authorize remote mutation. | `#120`, related `#30`, `#38`, `#82` |
| Shared event-store kernel | New alpha boundary | Prove one neutral envelope/receipt/conformance contract with separate Missis, Spy, and CSS adapters and separate stores. | No current Missis API break; extraction is gated on fixtures. | `#118` |

## 4. Exact proposed clauses

### V3-IDEM-001 — request-bound idempotency

For every mutation with an idempotency key, the authority MUST atomically
store one receipt and one versioned request fingerprint with the accepted
events. The fingerprint input MUST include:

- public operation name and version;
- actor after documented defaulting;
- the caller's public payload representation;
- explicit effective/known times, preconditions, and causal reference;
- content digests for supplied bytes; and
- a domain-separation prefix and fingerprint version.

It MUST exclude authority-assigned event IDs, aliases, stream sequences,
recorded time, and omitted time defaults. Version 1 does not collapse
equivalent aliases: a retry uses the same public request representation.

If the stored and proposed fingerprints match, the original receipt is
returned. If they differ, the request fails as `idempotency_mismatch` (conflict
exit class 5) and appends nothing. A format-v2 receipt predating request
fingerprints MUST NOT be backfilled from result/event data. Revision-3
migration moves it to a permanent key tombstone; guarded replay and reuse fail
closed and instruct the caller to use a new key.

Acceptance order is normative:

```text
1. Build the operation-specific v1 request envelope from caller inputs.
2. Canonically encode and hash that envelope before assigning event IDs/time.
3. Begin the authority transaction.
4. Look up the idempotency key.
   absent          -> validate and append
   same hash       -> return the stored receipt; do not revalidate current state
   different hash  -> idempotency_mismatch; append nothing
   v2 tombstone    -> idempotency_mismatch with new-key guidance
5. Insert accepted events, request hash, event IDs, and initial receipt in the
   same transaction.
6. Commit once; ambiguous response loss is resolved by repeating step 4.
```

Replay happens before current-state validation because the original command
may no longer be valid against the later projection even though it already
committed. A retry is asking for the prior receipt, not permission to execute
again.

The durable logical row is equivalent to:

```text
IdempotencyReceiptV1 {
  authority_namespace
  client_scope?             required before multi-client server use
  key
  request_hash_algorithm = "missis-request-v1"
  request_hash
  accepted_event_ids[]
  result_codec
  result_bytes
  created_at
}
```

The current local adapter scopes keys to one store. A server/multi-tenant
adapter MUST additionally scope them by authority namespace and authenticated
client (or another documented collision domain); making local global keys a
network-wide namespace would be an incompatible and unsafe assumption.

Required conformance cases are: identical retry, different operation, changed
payload, changed explicit time, changed precondition, response loss after
commit, two concurrent different requests with one key, format-v2 tombstone,
malformed/unknown hash version, and ingestion with identical/different content
digests.

### V3-HASH-001 — integrity epochs

Accepted `global-json-chain-v1` hashes are immutable evidence. A new canonical
hashing scheme MUST start a named/versioned integrity epoch or provide a
migration receipt that binds the prior-scheme head, new scheme, activation
cursor, and first new head.
Migration MUST NOT silently recompute old history in place.

Canonical-v1 bytes and test vectors defined by `#45` are complete. Adoption,
not definition, remains owned by `#57`.

Missis format 7 names the live successor `canonical-event-chain-v1`. A store
created directly at format 7 starts in that epoch. A migrated store retains
its unchanged `global-json-chain-v1` head until its first new append. That
append atomically writes `integrity-epoch-transition-v1`, binding the store
identity, prior epoch/head/event count/last cursor, target epoch, record codec,
first event/content digest/new head, format revision, and receipt digest. An
epoch may not regress or transition a second time. A historical row is not
backfilled with purported canonical bytes.

### V3-BYTES-001 — preserve exact accepted bytes

The authority MUST preserve the exact versioned canonical bytes whose digest
it accepts. Re-verification MUST hash those bytes, not reconstruct them from a
current native struct. SQLite `TEXT` or `BLOB` and PostgreSQL `BYTEA` are
adapter decisions; the portable contract is byte equality, codec version, and
unknown-field preservation.

Benefits:

1. runtime, language, field-order, and omission-default changes cannot alter
   old evidence;
2. independent implementations hash the same input;
3. unknown future fields remain auditable;
4. verification can distinguish unavailable interpretation from corruption;
5. recovery does not depend on reproducing an obsolete serializer.

`BLOB`/`BYTEA` expresses byte intent and avoids collation/transcoding hazards,
but a schema-type change without exact-byte preservation provides none of the
benefits above.

“Exact accepted bytes” means the byte sequence produced once by the declared
canonical codec after the authority has assigned accepted fields. It does not
mean arbitrary client JSON, a Go struct, SQLite's textual representation, or
the bytes of a later export.

The three byte domains remain separate:

| Byte domain | Purpose | Identity |
| --- | --- | --- |
| Request envelope bytes | Decide whether an idempotency key names the same attempted operation. | `request_hash` |
| Accepted event bytes | Immutable authority record after IDs/revisions/recorded time are assigned. | `content_hash` and integrity-chain input |
| Artifact bytes | Potentially large opaque content referenced by an event. | content-addressed artifact digest |

Acceptance and verification work as follows:

```text
validate proposal semantics
    -> assign record_id, stream_revision, recorded_at, batch_id
    -> canonical_codec_v1.encode(accepted envelope) exactly once
    -> hash those bytes with domain/version framing
    -> atomically persist codec, exact bytes, digest, indexes, and receipt
    -> decode the persisted bytes for synchronous projection deltas

later verification
    -> read persisted exact bytes
    -> verify digest and epoch/chain directly
    -> only then decode with the named codec/schema
```

A verifier MUST NOT load a native object and marshal it again to discover what
was accepted. That fails when a later binary drops unknown fields, changes an
omitempty/default rule, renames a property, changes timestamp precision, or
normalizes a value differently.

An adapter may physically store:

```text
accepted_records(
  record_id,
  codec_id,
  schema_id,
  canonical_payload,      -- TEXT, BLOB, or BYTEA by adapter
  content_hash,
  integrity_epoch,
  stream_revision,
  ...query indexes...
)
```

Indexes are redundant interpretations and MUST be checkable against decoded
canonical payloads. Unknown codec/schema versions fail as “unsupported but
bytes preserved,” not “corrupt,” unless the stored digest itself fails.

Current-state boundary: Missis format revision 7 preserves codec plus exact
accepted bytes and direct content digest for every newly accepted record.
Verification hashes those persisted bytes before decoding. Format-6 events
retain unchanged `event_json`, hash rows, head, and `global-json-chain-v1`
verification; their new exact-byte fields remain null rather than being
fabricated during migration.

### V3-DUR-001 — durability profile

An acknowledged commit MUST state or reference the active adapter profile.
The profile distinguishes at least:

| Class | Minimum meaning |
| --- | --- |
| process-crash recovery | Database recovers consistently after the Missis process stops under a still-running host. |
| host-power-loss durable | Every acknowledged commit is promised after OS crash/power loss under stated filesystem/storage assumptions. |
| replicated durable | Acknowledgment requires the configured independent-copy/quorum rule. |

The current local SQLite profile is `wal-normal`: atomic/consistent recovery,
but not a promise that the newest acknowledged commit survives host power
loss. Health MUST report the effective live pragmas. A `wal-full` profile may
be offered, but changing the default requires reproducible latency/throughput
evidence and disposable-VM fault evidence. The safe procedure lives in
`docs/durability-testing.md`.

### V3-PERF-001 — affected-key append work

For an append batch, storage work SHOULD be proportional to the accepted
events, affected logical-key histories, and rows whose semantic projection
actually changes. It MUST NOT claim amortized constant work when the adapter
refolds the complete touched stream or rewrites every current part.

Incremental implementation MUST preserve the result of a deterministic full
fold, including backdated valid time, known-time visibility, retraction,
supersession, ordering, recursive retraction, and subtree moves. A subtree move
may legitimately cost `O(descendants)` because every descendant path changes.
Current-link preconditions SHOULD use an indexed rebuildable projection.

#### What the current Missis append actually does

For every distinct touched stream, the SQLite transaction currently:

```text
SELECT every prior event in the stream ordered by sequence
    -> decode every event
    -> validate each proposed event against the growing complete slice
    -> for link guards, optionally load every event in the whole ledger
    -> append event rows and the `global-json-chain-v1` hash chain
    -> fold the complete touched ticket stream into CurrentProjection
    -> upsert the ticket summary
    -> DELETE every parts_current row for that ticket
    -> INSERT every currently visible part again
    -> commit once
```

This is correct and transactionally simple, but a one-part status update pays
for old events and unchanged current parts. Let `s` be touched-stream history,
`p` current ticket parts, and `l` total ledger history for a guarded link. The
dominant work is approximately `O(s log s + p)`, or `O(l)` for the link guard,
not amortized constant work. Ticket `#61` removed dependence on unrelated
history for ordinary non-link append; it did not remove these touched-stream
costs.

#### Target affected-key flow

The domain adapter first describes semantic impact without changing the public
request or event result:

```text
Impact {
  stream_id
  logical_keys[]            e.g. one Part ID/status key
  current_link_triples[]
  containment_roots[]       subtree expansion when paths really change
  projection_names[]
}
```

Inside the same append transaction the adapter then:

```text
read stream head and only required current/version rows
check target/link/containment preconditions using indexes
assign and insert accepted events
insert part_versions/link_versions entries
for each affected logical key:
    select the temporal winner from that key's version index
    compare with parts_current
    upsert/delete only when the projected row changed
for a move:
    enumerate the current subtree and update exactly its changed paths
advance projection watermark and stream head
commit events + indexes + deltas + receipt atomically
```

`part_versions` is rebuildable metadata mapping a stable logical Part ID/key to
candidate event IDs and their effective/recorded/sequence ordering fields.
`links_current` is a rebuildable index keyed by canonical `(from, relation,
to)` plus the active assertion event. `parts_current` remains the current
projection, not authority. A fresh complete fold remains the recovery and test
oracle.

#### Behavior that must remain identical

- backdated events may replace the current winner for only the affected key;
- a supersession recomputes every key influenced by the superseded event;
- retraction may reveal an older winner rather than merely deleting a row;
- moving a part changes descendant paths and is intentionally
  `O(descendant count)`;
- recursive retraction touches the declared subtree and remains atomic;
- order-key changes preserve deterministic sibling order and dense-rebalance
  semantics;
- link evidence multiplicity and expected-assertion conflicts are unchanged;
- a failed delta writes neither event nor partial projection.

The safe implementation technique is differential testing: apply identical
generated event sequences to the incremental reducer and the existing full
fold, then compare every projected row, conflict, and affected path. Measure
events decoded, rows read/written, WAL bytes, and writer transaction duration
for long histories, large current documents, link ledgers, backdating,
supersession, and subtree moves before and after. Ticket `#119` owns those
gates.

### V3-REF-001 — canonical cross-store references

A reference to another Missis repository/store cannot use `project#184` as
identity. Project labels and `#184` aliases can collide and can be renamed.
The canonical v1 form is structured data:

```json
{
  "version": "external-ref-v1",
  "store_id": "store:v1:sha256:<64 lowercase hex digits>",
  "namespace": "missis",
  "kind": "ticket",
  "entity_id": "ticket:01M...",
  "subentity_id": null,
  "pin": null,
  "observation": {
    "stream_revision": 12,
    "current_event_id": "event:01M..."
  },
  "display_hint": "spy-testing#42"
}
```

Required identity fields are `version`, immutable `store_id`, consumer
`namespace`, `kind`, and canonical `entity_id`. For a Part, `subentity_id` is
its canonical Part ID; the path is only a display hint. `pin.event_id` pins an
exact immutable historical record. `pin.checkpoint_digest` optionally pins or
verifies the foreign store state against which a claim was made. These two
cases are distinct:

```text
entity reference       follows the same canonical entity as its state evolves
event/checkpoint ref   cites immutable evidence at one accepted store state
```

`display_hint` may contain a repository label, alias, or path for humans, but
is inert text. Paths, URIs, hostnames, and credentials are excluded from the
accepted reference. Only a local/transport adapter that constructs an
already-authorized peer handle may see location; it never participates in
identity or reference hashing.

Resolution is local policy:

```text
same store_id
    -> use the already-open store
supplied local peer
    -> query its identity claim, verify it, then read through the already-open handle
supplied remote peer
    -> query only when policy authorizes it; verify identity/checkpoint evidence
no reachable peer claims the requested ID
    -> retain and display an unavailable foreign reference; never retarget by alias
```

There is no central registration architecture. `PeerResolver` receives opaque
already-authorized handles and asks each for a fresh identity/state claim.
Only the adapter that constructed a handle sees a filesystem path or transport.
Machine-specific locations MUST NOT be embedded in accepted event identity.
There are no SQLite foreign keys across databases. Copying/restoring a store
preserves its identity document; a deliberately writable fork receives a new
identity plus lineage under `#122`/`#48`/`#38` rather than quietly presenting
the same writable authority.

Cross-store **read/navigation and links** can be added before synchronization.
Cross-store mutation, cascade, or transaction atomicity is not implied and is
forbidden until an authority protocol defines it. The identity, resolver,
staleness/retraction, fork, security, cross-repository ticket workflow, and
test matrix are expanded in `cross-store-references-v3-alpha.md`. Ticket
`#120` owns that draft; `#30` owns navigation UI and `#82` owns scoped human
aliases.

The cross-store draft also owns the normative `fail-*` vocabulary. In summary:
malformed input fails fast; absent/conflicting proof fails closed; durable
references and accepted evidence fail safe; a corrupt authority fails stop;
fail-over requires identity plus replica-state proof; and fail-open is
forbidden. Displaying explicitly labelled degraded historical data is
`open-with-degraded-trust`, not permission to use it for mutation, completion,
or evidence verification.

### V3-START-001 — startup integrity boundary

For the current local format, normal open continues fail-fast complete-chain
verification. Projection freshness MAY be bounded by validated version and
watermark metadata without changing query results.

The current order is compatibility probe, WAL/pragmas, forward migrations,
complete `global-json-chain-v1` verification in one transaction, then derived-head
and ordered-projection drift inspection/repair. This means a normal short CLI
command may scan every accepted event before reading one ticket. It also means
old chain corruption is detected on every successful open.

A future checkpoint-plus-tail startup is an intentional detection-timing
change, not a transparent performance refactor. It requires:

- a stated attacker/corruption threat model;
- a checkpoint whose trust does not derive solely from the unchecked ledger;
- tail verification from that checkpoint;
- an explicit non-mutating complete verifier; and
- health/status wording that identifies the verified range.

The hypotheses, corruption matrix, benchmark sizes, checkpoint trust rules,
health fields, and adoption gates are defined in the dedicated protected draft
`startup-integrity-v3.subspec.md`. That draft is experimental context, not
permission to weaken current open behavior.

## 5. Implementation mapping without API drift

| Problem | Internal rewrite | Preserved behavior | If behavior must break |
| --- | --- | --- | --- |
| Complete touched-stream refold | Part-version/current-event indexes plus affected-key reduction | Same projected state and conflict results | Change winner semantics only through an approved spec clause; benefit must exceed migration cost. |
| Delete/reinsert complete ticket projection | Transactional row delta checked against full-fold oracle | Same atomic visibility and ordering | No known break is necessary. |
| Complete-ledger link check | `links_current` indexed by canonical triple and assertion event | Same evidence/precondition semantics | No known break is necessary. |
| Ordered drift refold on open | Versioned projection watermark and targeted repair | Same data returned after open | If stale reads are ever allowed, expose them explicitly; do not silently weaken open. |
| Full-chain scan on open | Deferred checkpoint-plus-tail verification | Current fail-fast rule remains for now | Benefit: bounded startup. Tradeoff: older corruption detection moves to deep audit; requires `#58` and `#110`. |

## 6. Evidence required before promotion

- `#116`: characterization test passes on old behavior, desired rejection test
  fails before the fix, then the tests swap after implementation; migration
  fixture proves the format-v2 tombstone boundary.
- `#57`: persisted hash integration fixture and epoch/receipt test vectors.
- `#117`: live health settings, process-crash boundary tests, disposable-VM
  plan, and strict-profile benchmark.
- `#119`: rows read/written and WAL bytes for long streams, large part sets,
  link ledgers, backdated events, supersession, and subtree moves.
- `#109`/`#110`: startup timings, watermark corruption cases, and explicit
  non-mutating audit evidence.
- `#120`: two-store alias-collision, offline/unresolved, moved-Part,
  event/checkpoint pinning, fork, and malicious-locator fixtures.
- Consumer-neutral alpha: at least one Missis, Spy, and CSS fixture passes the
  same envelope/idempotency/exact-byte/backup conformance suite while retaining
  separate domain projections.

## 7. Promotion and contract maintenance checklist

1. Review each incompatible clause and increment the concrete alpha version.
2. Keep the frozen v2 product/domain contract separate; update Phase 1
   requirements and the registry only when their scopes change.
3. Update `docs/storage-compatibility.md` and retained fixtures for durable
   format changes.
4. Move unmet work into focused linked tickets; do not leave unchecked
   criteria on a done ticket.
5. Preserve released alpha contracts by version when a later alpha replaces
   them; do not move new event-store behavior back into frozen v2.

### 7.1 Alpha history

- `eventstore-v3-alpha.1`: introduced request-bound idempotency, hashed store
  identity, strict durable external references, and the initial supplied-peer
  resolver contract.
- `eventstore-v3-alpha.2`: makes v3-alpha authoritative for new event-store
  work, replaces separate claim/resolve authority calls with one verified
  resolution snapshot, and defines `sqlite-live-readonly-v1` configuration,
  inspection, navigation, and explicit WAL/SHM coordination behavior.
- `eventstore-v3-alpha.3`: adds physical format revision 6,
  `artifact-namespace-fork-v1`, `store-identity-fork-v2`, independent CAS
  copying, durable manifest/completion markers, inspection, and restart-safe
  recovery. External-reference resolution semantics remain one-way/read-only.
- `eventstore-v3-alpha.4`: adds physical format revision 7,
  `eventstore-record-json-v1`, exact accepted-byte/content identity,
  `canonical-event-chain-v1`, and receipt-bound mixed-epoch activation while
  preserving all format-6 event and hash evidence unchanged.
