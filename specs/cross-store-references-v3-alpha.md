# Cross-store references and resolution for event-store v3-alpha

**Status:** authoritative v3-alpha cross-store reference contract  
**Owner:** ticket `#120`  
**Protocol version:** `eventstore-v3-alpha.3`  
**Related:** `#30`, `#38`, `#48`, `#58`, `#82`, `#118`  
**Date:** 2026-08-27

This contract defines how one Missis, Spy Testing, or CSS Flight Recorder store
can retain and navigate a reference to evidence in another store without
embedding filesystem layout, trusting aliases, or silently hiding stale or
retracted state. It is subordinate to `event-store-v3-alpha.md`; incompatible
changes increment the concrete event-store alpha protocol version.

## 1. Boundary and confirmed problem

Confirmed:

- a Missis `#42` alias is allocated inside one store and can collide with
  `#42` in another store;
- project/repository labels and Part paths can be renamed and are not global
  identity;
- current `Ref` values do not carry foreign `store_id`, so they cannot prove
  which authority an apparent cross-project reference names;
- SQLite foreign keys cannot enforce identity or lifecycle across databases;
- filesystem paths are machine-specific capabilities, not portable identity.

Therefore `project#42`, a relative repository path, a database filename, and
a URL are never canonical foreign identity. The alpha boundary supports
durable references plus read-only resolution/navigation. It does not promise
cross-store atomic writes, cascades, synchronization, or remote mutation.

### 1.1 Store identity: global uniqueness without a central registry

No finite identifier can mathematically guarantee that a collision is
impossible. The v3 contract instead requires both negligible accidental
collision probability and positive detection when two peers claim the same
identifier with incompatible evidence.

Every v3 store is created from an immutable `StoreIdentityDocumentV1`:

```text
version = store-identity-document-v1
scheme = eventstore-hash-v1
nonce = 32 cryptographically random bytes
```

Canonical bytes include only `version`, `scheme`, and `nonce`, using unsigned
big-endian length framing. Creation time, creator protocol, and contract bundle
digest are separately stored provenance and do not affect permanent identity.
The identifier is:

```text
digest = SHA256(
  "MISSIS-EVENTSTORE-IDENTITY" || 0x00 || "v1" || 0x00 ||
  canonical_identity_document_bytes
)
store_id = "store:v1:sha256:" + lowercase_hex(digest)
```

The full 256-bit digest is retained; filenames, repository names, Git remotes,
machine IDs, usernames, timestamps, and process IDs never contribute identity.
Cloning, moving, nesting, or checking out a repository therefore cannot alter
`store_id`. Mutable event content is deliberately excluded: an append changes
the head claim, not the identity of the store.

`vectors/store-identity-v1.json` publishes the domain bytes, field framing,
nonce, complete canonical document, document digest, and resulting store ID.
The Go codec and the independent Node.js verifier
`vectors/verify-store-identity-v1.mjs` both recompute the result; Node-capable
test runs execute that verifier. These files pin settled byte mechanics while
this Markdown remains the authoritative explanation of intent.

The identity document is stored durably with the database and is included in
backup/restore. A peer recomputes `store_id` before accepting its claim. A
writable fork receives a new identity document and records lineage to the
origin; a read-only replica preserves the original document. Pre-v3
`missis-ulid-v1` IDs remain explicit migration inputs only. They do not satisfy
the v3 self-certifying identity guarantee and cannot originate a durable v3
external reference until an identity migration receipt binds the old ID,
new ID, source head, event count, and contract digest.

The implemented alpha operation is deliberately version-targeted:

```text
missis-tools store fork plan \
  --store COPY \
  --to-identity-version 1

missis-tools store fork apply \
  --store COPY \
  --to-identity-version 1 \
  --from-store-id store:v1:sha256:<expected-parent> \
  --backup PRE_FORK.db
```

`plan` is read-only. `apply` takes an exclusive store lease, rejects a stale or
wrong `from-store-id`, creates and hashes the rollback database, verifies the
ledger, generates a fresh identity document, and atomically changes
`store_meta`, the current identity document, and a `store-identity-fork-v1`
receipt. The receipt binds the exact parent identity document/digest, parent
head/count/integrity epoch, format revision, new identity/document digest,
backup digest, and artifact disposition. The latest receipt digest is exposed
in backup manifests and peer identity claims; it provides lineage evidence but
does not by itself grant authority to the parent or child.

The alpha operation is confirmed only when three exact inventory classes are
zero: artifact-index rows, managed CAS-reference occurrences in typed accepted
events, and unmanaged artifact-kind source-identity occurrences in typed
accepted events. It also reports how many accepted events contain either
occurrence class. A zero inventory assigns the child ID a new empty artifact
namespace. It does not use a substring scan. Any non-zero gating count fails
before backup or mutation with every count and points to #115.

The namespace-copy/source-disposition operation remains deliberately
unimplemented. Its frozen
precondition is: copy and independently verify every accepted object into a
staged child namespace; write a sorted reference/event manifest and digest;
write the completion marker last; atomically publish the namespace; only then
change child identity/namespace and record a receipt binding parent/child
identity, head/epoch/count, old/new namespace, manifest digest/counts, backup
digest, and disposition. On restart, no marker is incomplete staging; marker
without receipt is prepared/uncommitted; matching receipt/marker/manifest is
complete; receipt with missing/mismatched namespace is an integrity incident
requiring pre-fork database restoration. The source namespace is never deleted
by the fork. The exact steps and fault matrix are in
`docs/artifact-integrity-and-recovery.md`, section 6. Repository location,
machine location, and directory copying never imply lineage or select an
authority.

Hash-derived identity proves that an identity document corresponds to its
`store_id`; it does **not** authenticate the peer presenting that document. An
exactly copied document produces the same ID by design and is a candidate
replica, not a collision. Peer authorization, transport identity, signed
claims, key rotation, and checkpoint trust are separate authority concerns
owned with `#48`/`#58`. A local already-open handle can be trusted by explicit
local policy; a remote peer cannot gain authority merely by knowing or copying
an identity document. Store identity does not change when an authority key or
transport endpoint rotates.

## 2. Canonical reference

The accepted logical form is:

```text
ExternalRefV1 {
  version = "external-ref-v1"
  store_id                     immutable authority/store identity
  namespace                    consumer namespace, e.g. missis | spy | css
  kind                         schema-defined entity kind
  entity_id                    canonical immutable entity identity
  subentity_id?                canonical Part/observation/etc. identity
  pin? {
    event_id?                  exact immutable accepted evidence
    checkpoint_digest?         exact authority state/proof boundary
  }
  observation? {
    stream_revision?
    current_event_id?
    observed_at?
  }
  display_hint?                inert human label only
}
```

Identity is the tuple `(version, store_id, namespace, kind, entity_id,
subentity_id, pin)`. `display_hint` and `observation` do not retarget it and do
not participate in reference equality. The reference contains no path, URI,
hostname, repository name, bearer token, or command.

The optional `observation` records what the source saw and supports a cheap
staleness comparison. It is not proof by itself. A claim that requires exact
historical evidence MUST pin an `event_id`, a trusted `checkpoint_digest`, or
both; an unpinned entity reference deliberately follows the entity's current
state.

### 2.1 Tracking versus pinned references

| Form | Meaning | Later target changes |
| --- | --- | --- |
| Entity reference | “This canonical entity in that store.” | Resolution returns its newest state under the selected temporal projection and reports whether the prior observation is stale. |
| Event pin | “This exact immutable accepted record.” | It remains resolvable even if current entity state is retracted; the resolver reports both historical evidence and current lifecycle separately. |
| Checkpoint pin | “This claim was evaluated against this exact authority state.” | A different/missing checkpoint is a mismatch or unavailable result, never an automatic refresh. |
| Entity + observation token | Tracking reference with last-seen revision/current event. | A changed token is `stale`; caller must explicitly accept/re-evaluate new state. |

### 2.2 Strict durable-value algorithm

Format revision 5 implements `external-ref-v1` as a built-in durable value,
not as an embedded filesystem locator. Acceptance is exactly:

1. decode one JSON value with unknown-field rejection and reject trailing JSON;
2. require `version == external-ref-v1`;
3. validate the full lowercase `store:v1:sha256:<64 hex>` identity grammar;
4. require non-empty, whitespace/control-free `namespace`, `kind`, and
   `entity_id`, and apply the same token rule to optional IDs/digests;
5. require a pin to contain an event ID, checkpoint digest, or both;
6. require an observation to contain a stream revision, current event ID,
   observation time, or a combination;
7. coerce SDK/CLI maps through this same strict codec before validation;
8. append the typed value through the ordinary atomic event/idempotency path;
9. deserialize accepted history back into `ExternalRefV1`, failing rather than
   silently retaining an unknown shape as an untyped map;
10. render the unresolved reference with store/entity identity and inert
    display text; absence of a reachable peer never removes or retargets it.

The canonical identity key uses length-framed fields in this order:
`version`, `store_id`, `namespace`, `kind`, `entity_id`, `subentity_id`,
`pin.event_id`, `pin.checkpoint_digest`. Observation and display fields are
excluded. `specs/vectors/external-ref-v1.json` pins accepted JSON and the
identity key. The format-5 compatibility fixture pins close/open, backup, and
typed event decoding; CLI black-box tests prove process-to-process persistence
and rejection of embedded locators.

## 3. Filesystem-neutral peer discovery and authority resolution

There is no central store registry and no registration protocol. A process is
given zero or more already-authorized peer handles by its environment, local
collector, explicit user action, or authenticated transport. Accepted content
cannot create such a handle. Domain code queries peer claims and never
navigates a filesystem:

```text
PeerResolver.Resolve(ctx, ExternalRefV1, query) -> ResolutionV1
PeerHandle.OpenExternalResolutionSnapshot(ctx) -> PeerSnapshot
PeerSnapshot.StoreIdentityClaimContext(ctx) -> StoreIdentityClaimV1
PeerSnapshot.ResolveExternalReferenceContext(ctx, ExternalRefV1, query) -> ResolutionV1
PeerSnapshot.Close()

Future optional capability interfaces:
PeerHandle.FetchEvent(ctx, store_id, event_id) -> EvidenceV1
PeerHandle.Checkpoint(ctx, digest) -> CheckpointProofV1
PeerHandle.Changes(ctx, cursor, filter) -> ChangePageV1
```

The same peer interface may be implemented by:

```text
already-open local SQLite read-only handle
local collector/service peer
explicitly authorized remote peer
```

Only an adapter knows whether its opaque handle currently reaches a path or
network authority. Domain code, stored references, exports, links, and agent
prompts see only `store_id` and the opaque peer. Moving a repository changes
how an authorized handle is constructed; it never rewrites accepted events or
updates a central mapping.

On every resolution the resolver asks every reachable candidate for a
`StoreIdentityClaimV1`, verifies its identity document/digest, and compares the
claim with the requested `store_id`. Local foreign stores open read-only and
must not run migrations, change WAL mode, repair projections, or claim a writer
lease. A `display_hint`, imported event, web page, remote response, or embedded
locator cannot create or authorize a peer.

“Unregistered store” is not a valid state. With zero matching claims the result
is `unavailable/unknown` plus the precise statement “no reachable peer claimed
the requested store identity.” It means only that this resolver invocation had
no usable peer; it does not assert that a global registry lacks an entry.

### 3.1 Peer identity and state claim

```text
StoreIdentityClaimV1 {
  version
  store_id
  identity_scheme
  identity_document
  identity_digest
  genesis_digest
  genesis_digest_scheme
  head_digest
  head_integrity_epoch
  event_count
  format_revision
  protocol_version
  contract_bundle_digest
  checkpoint_digest?
  signed_at?
  signature?                  required for authenticated remote claims
}
```

The identity document proves how `store_id` was derived. Genesis identifies
the immutable first accepted boundary and names its digest scheme. Head/count
describe mutable present state and name the integrity epoch that produced the
head. Digests from different schemes/epochs are not directly compared as
bytes: the peer must supply a versioned transition proof or the result is
`unsupported-integrity-transition`, not `identity-collision`. A checkpoint or
signature can add rollback and remote-authentication assurance but does not
replace identity verification.

Each inspected peer produces a diagnostic comparison containing the expected
ID, claimed ID, identity scheme/digest, genesis digest, head digest, event
count, format revision, classification, and explicit differing fields. Raw
paths, URIs, credentials, and transport errors are not returned.

| Comparison | Classification and action |
| --- | --- |
| Different claimed `store_id` | `different-store`; retain the candidate insight but do not call its entity resolver. |
| Same ID, identity document cannot recompute ID | `invalid-claim`; fail closed. |
| Same ID, different identity/genesis evidence | `identity-collision`; fail closed and select no peer. |
| Same identity, same head and count | `exact-replica`; either peer may serve the read under local policy. |
| Same identity/genesis, different head or count, ancestry unknown | `divergent-state-unverified`; fail closed and select no peer. |
| Same identity, ancestry proves one head extends the other | `stale-replica`/`advanced-replica`; select only according to explicit freshness/checkpoint policy. |
| Claim cannot be obtained | `unreachable`; preserve the underlying transport detail only in protected operator diagnostics. |

### 3.2 Operator-configured local SQLite peer (implemented Linux alpha.2; retained in alpha.3)

This is peer construction, not store registration. A local binding gives one
process permission to attempt a read-only connection; it neither allocates a
`store_id` nor asserts that the named database owns one. The peer must prove
its claim from the database on each resolution. The implemented local-only input
is:

```json
{
  "version": "missis-local-peer-set-v1",
  "peers": [
    {
      "handle": "spy-local",
      "adapter": "sqlite-live-readonly-v1",
      "expected_store_id": "store:v1:sha256:<64 lowercase hex>",
      "sqlite_path": "/operator/configured/path/spy.db"
    }
  ]
}
```

The file is process configuration, not accepted ledger data, an export, a
backup member, or a shared authority registry. `handle` is an opaque local
diagnostic label and never appears in `ExternalRefV1`. `expected_store_id` is
mandatory operator intent: it catches a moved, swapped, or mistyped database
before entity resolution, but the peer's recomputed claim remains the evidence.
`sqlite_path` is visible only to the local adapter and protected operator
inspection. Moving a store changes this field and nothing in accepted history.
Duplicate handles, unknown fields/versions/adapters, relative paths, missing
expected IDs, and group/world-writable configuration on Unix are rejected
before a database is opened. Native Windows ACL and reparse-point enforcement
is not confirmed and returns `peer-platform-unsupported`; #112 owns that
evidence.

Multiple bindings may expect the same `store_id` because exact read-only
replicas are legitimate. They are not silently coalesced. The resolver obtains
and compares every claim so exact replicas, unproven divergence, and identity
collision remain visible. A binding whose database claims another ID produces
`different-store` insight with expected and claimed IDs plus identity/genesis/
head evidence; it is not a blunt path error and its entity resolver is never
called. Resolver-facing insight remains path-free. A protected command does
show the local handle and normalized configured path alongside the claim:

```text
missis-tools peers inspect --config PEERS.json --json
missis show REF --resolve-external --peer-config PEERS.json
```

The implemented algorithm is:

1. Strictly decode and validate the complete peer-set file before filesystem
   access. Durable reference fields are never interpreted as configuration.
2. Normalize only the configured absolute path, require a regular SQLite file,
   reject a final-component symlink in the alpha adapter, and retain the raw
   path only in protected local diagnostics.
3. Open the existing coordination lock read-only and take a shared lease
   without creating or rewriting the lock. If safe coordination cannot be
   obtained, return `coordination-unavailable`; do not fall back to an
   uncoordinated live read. A separately versioned immutable-backup adapter may
   later use a verified bundle completion marker instead of a live-store lease.
4. Open SQLite with `mode=ro`, enable connection-local `query_only`, and begin
   one read transaction. Do not configure WAL, migrate schemas, repair or read
   from rebuildable projections, initialize an artifact root, checkpoint WAL,
   or acquire a writer lease. A live WAL reader may update SQLite's existing
   `-wal`/`-shm` reader-coordination state and can create those sidecars when a
   clean WAL-mode database has none; they are not accepted or derived store
   data. The adapter may not create the maintenance lock or any other path, or
   alter database/accepted WAL bytes. A later `sqlite-sealed-snapshot-v1`
   adapter may use `immutable=1` only
   for a verified, completed backup with no unapplied WAL.
5. Probe format and require the supported current read format. Recompute the
   hashed identity binding and verify the complete event chain inside that same
   read transaction. A corrupt peer fails stop and serves no entity state.
6. Produce the identity/head/count claim and fold the requested ticket/Part
   directly from accepted events in the same read transaction. This avoids
   trusting projection drift and prevents an append between claim and entity
   resolution from mixing two store states.
7. Close the read transaction, database connection, and lease on success,
   failure, timeout, or cancellation. Return only structured resolution and
   peer insight; never return the configured path through the domain API.

The prior `ExternalAuthority` interface performed claim and resolution as two
calls. A characterization test confirmed that it could mix pre-append claim
evidence with a post-append entity result. Alpha.2 removed that interface and
uses this session boundary:

```text
ExternalAuthority.OpenExternalResolutionSnapshot(ctx) -> ExternalAuthoritySnapshot
ExternalAuthoritySnapshot.StoreIdentityClaimContext(ctx) -> StoreIdentityClaimV1
ExternalAuthoritySnapshot.ResolveExternalReferenceContext(ctx, ref, query) -> ResolutionV1
ExternalAuthoritySnapshot.Close()
```

All matching-peer comparison still occurs before selecting an authority. The
resolver opens one bounded snapshot per candidate, compares claims, resolves
through the selected matching snapshot, then closes every snapshot. The old
two-call interface no longer exists.

Stable local failure codes are: `peer-config-invalid`,
`peer-not-found`, `peer-permission-denied`, `coordination-unavailable`,
`peer-format-unsupported`, `peer-migration-required`, `different-store`,
`peer-integrity-failed`, `peer-platform-unsupported`, `peer-timeout`, and
`peer-cancelled`.
Only not-found, busy, timeout, cancellation, and temporarily unavailable
coordination are normally retryable. Configuration, identity, format, and
integrity failures require the specific correction/migration/recovery stated
in operator output. None retargets or deletes the durable reference.

Hermetic completion requires all of the following:

| Case | Required proof |
| --- | --- |
| Move peer database | Update only `sqlite_path`; accepted `ExternalRefV1` bytes and source ledger remain unchanged and resolution succeeds by the same `store_id`. |
| Read-only enforcement | Database bytes, accepted WAL transactions, schema, projections, artifact tree, and maintenance-lock contents remain unchanged. Only SQLite `-wal`/`-shm` coordination sidecars may be created or changed; atime is not a semantic guarantee. Tests enumerate the directory and reject every other new path. |
| Snapshot coherence | A synchronized writer appends between candidate inspection steps; each result contains claim and entity state from one pre-append or post-append snapshot, never a mixture. |
| Projection drift | Deliberately drift derived rows; peer resolution folds accepted events, reports correct state, and performs no repair. |
| Wrong database at configured path | Report expected/claimed IDs and differing identity/head fields; perform zero entity queries. |
| Exact and divergent replicas | Exact claims permit deterministic read-only fail-over; different unproven heads select neither peer. |
| Malicious accepted content | Path, URI, credential, or locator fields fail strict reference parsing and the instrumented peer opener records zero calls. |
| Lifecycle after reopen | Current, stale, retracted, pinned/unsupported-checkpoint, missing, and unavailable behavior are covered by resolver/application tests; the black-box moved-store test proves close/open persistence and unchanged accepted identity. Timeout/cancellation are stable access failures covered separately, not persisted lifecycle state. |
| Old/corrupt store | Return migration-required or integrity-specific fail-stop diagnostics and prove zero bytes changed. |

#### 3.2.1 Execution plan and merge gates

Implementation used the following ordered slices. Orders 0–6 and their Linux
tests are confirmed and retained in alpha.3; a later slice may not weaken an earlier gate.

| Order | Code boundary | Work | Gate |
| --- | --- | --- | --- |
| 0 — reproduce | `pkg/missis/external_reference_test.go` | Add a fake authority that changes state between the current claim and resolve calls. First prove the current mixed-snapshot behavior is possible; then add the desired invariant test, which must fail before the API change. | The test distinguishes old-claim/new-entity mixing from a coherent pre- or post-append result. |
| 1 — snapshot API | `pkg/missis/external_reference.go`, `pkg/missis/client.go` | Replace `StoreIdentityClaimContext` + `ResolveExternalReference` on an authority with `OpenExternalResolutionSnapshot`. The snapshot exposes context-aware claim/resolve and idempotent `Close`. `PeerResolver` opens every candidate, compares claims, resolves with the selected still-open snapshot, and closes all candidates on every exit. | Unit tests cover zero peers, open/resolve failure, invalid claim, wrong ID, exact/divergent peers, and exactly-once resolver close. Store tests cover idempotent snapshot close. The old two-call interface is deleted in this alpha change. |
| 2 — read-only store substrate | new `internal/store/read_snapshot.go`; helpers in `store.go` and `maintenance_lock.go` | Add `AcquireExistingSharedLeaseReadOnly`, which uses an existing lock and never creates the lock/directory. Add a reader type containing one `*sql.DB`, one read-only `*sql.Tx`, and the lease—never a writer DB. Factor context-aware identity, full-chain verification, head/count, lineage receipt, and stream-load helpers to operate on the transaction. | Tests prove current-format acceptance, older-format rejection, corrupt-chain fail-stop, required pre-existing coordination, idempotent close, no projection reads/repair, unchanged database/lock content, and only permitted SQLite WAL/SHM sidecars. A newer-format fixture is not yet retained. |
| 3 — Missis peer adapter | new `internal/application/local_peer.go`; reuse pure fold helpers from `external_reference.go` | Construct an opaque Missis authority from the read-only store substrate. Claim and ticket/Part resolution use the same transaction and explicit query times; no full `Service`, schema mutation, plugins, artifact root, or wall-clock default hidden inside the adapter. | Integration tests synchronize a writer append while a peer snapshot is open and prove one coherent state; projection rows may be deliberately wrong without affecting the result. |
| 4 — strict local binding codec | new `internal/peerconfig` package | Decode at most 1 MiB and 32 peers with unknown-field/trailing-data rejection. Require version, unique 1–64 character opaque handles, `sqlite-live-readonly-v1`, strict expected hashed store ID, and cleaned absolute path. On Unix require the config and database parent not be group/world writable and reject a final symlink. No implicit directory scan or environment discovery. | Tests cover strict valid decode, the file-size and peer-count boundaries, unknown/trailing fields, duplicate handles, relative paths, permission rejection, and final symlink rejection. Config never appears in accepted events, backups, exports, or reference identity. |
| 5 — operator inspection | new `internal/tooling/peers.go`, `tools/missis-tools/main.go` | Add `missis-tools peers inspect --config FILE [--timeout D] [--json]`. Protected output may show handle/path; it must report expected and claimed IDs, identity/genesis/head evidence, format, classification, retryability, and recourse. Exit non-zero for malformed config or any invalid/integrity claim; offline peers remain individually classified. | Unit and CLI tests cover verified and wrong-store output, exact claim evidence, recourse, strict command arguments, and exit status. Timeout/cancellation classification is covered at the adapter boundary; a deterministic blocked-I/O integration fixture remains hardening evidence. |
| 6 — navigation | `cmd/missis/main.go`, public client composition, black-box tests | Add `missis show LOCAL_REF --resolve-external --peer-config FILE [--peer-timeout D]`. `LOCAL_REF` must resolve to an accepted `external-ref-v1` value in the source store; raw paths or ad-hoc aliases are not accepted as targets. Default `show` behavior is unchanged when the flag is absent. | The two-store black-box test accepts a reference, inspects the target, resolves it, moves the target tree, changes only config, and resolves the unchanged source bytes again. Existing structured-value tests cover malicious locator rejection; application tests cover stale, retracted, and unsupported checkpoint behavior. |
| 7 — platform classification | #112 plus build-specific peer-config/lease tests | Linux local filesystems are the first confirmed profile. Native Windows remains `peer-platform-unsupported` until ACL ownership, reparse-point, read-only WAL/SHM, and lock tests pass. WSL/DrvFS and network filesystems remain unsupported unless #112 supplies equivalent evidence. | Help, health/inspection, and release documentation state the active classification; an untested platform cannot silently claim verified local-peer support. |

The local slice has no store-format migration: peer configuration and
resolution state remain outside accepted data. It deliberately breaks only the
alpha Go authority interface and adds opt-in CLI flags/commands. Existing
`show`, writes, event bytes, store identity, backup format, and unresolved
rendering remain unchanged.

Completion for the local #120 slice means orders 0–6 pass on the confirmed
Linux profile, full `go test ./...` and `check-done` pass, and #112 owns the
explicitly unsupported platform matrix. Change feeds, authenticated remote
peers, checkpoint proofs, sealed snapshots, and reciprocal writes are not part
of this completion gate.

### 3.3 Failure-policy vocabulary

The `fail-*` terms describe how the resolver reacts; they are not interchangeable
result codes. In particular, **fail closed does not mean crash, discard the
reference, or call the target missing**. It means the resolver refuses to grant
an assurance or perform a dependent action whose preconditions were not proven.

| Policy term | Exact meaning in this contract | Allowed | Forbidden |
| --- | --- | --- | --- |
| `fail-fast` | Reject a malformed/unsupported request before peer resolution or mutation. | Return a stable validation/capability error with the rejected field/version. | Network/filesystem discovery, cached fallback, or partial writes. |
| `fail-closed` | Preserve evidence but deny verified/current/active assurance or peer selection when required proof is absent or conflicting. | Structured state, diagnostics, retry/migration guidance, explicitly labelled historical display. | Treating unknown, stale, conflicting, or unauthorized state as verified. |
| `fail-safe` | Leave durable source/target state unchanged and retain enough information for retry/reconciliation. | Keep `ExternalRefV1`, last observation, and protected diagnostic; retry read-only work under policy. | Deleting/retargeting the reference or cascading a remote change. |
| `fail-stop` | A peer that cannot validate its own identity/integrity stops serving verified results. | Health/inspection output and recovery guidance from a non-authoritative diagnostic path. | Entity resolution, checkpoint issuance, or mutation as that authority. |
| `fail-over` | Select another peer only after it proves the same immutable identity and an acceptable state relationship. | Exact replica, or ancestry/checkpoint-proven stale/advanced replica selected by policy. | Choosing by path order, response speed, modification time, label, or highest unproven event count. |
| `open-with-degraded-trust` | Explicit caller policy for navigation/presentation when authority or freshness proof is incomplete. It is not fail-open. | Display cached/returned data labelled `unverified`, with the failed checks visible. | Completion gates, mutation authorization, evidence verification, current/active claims, or cache promotion. |
| `fail-open` | Silently continue as though a failed check passed. | Nothing for identity, authorization, integrity, checkpoint, lifecycle, or mutation decisions. | Required checks defaulting to success, aliases retargeting, cached active state presented as current, or unverified replicas becoming authoritative. |

### 3.4 Exhaustive resolver failure matrix

Every resolution failure maps to one primary code, policy, retry classification,
and recovery action. Adding an implementation error without a row is an
underspecified protocol change and increments the alpha contract version.

| Scenario | Result/classification | Policy | Retry/recovery |
| --- | --- | --- | --- |
| Malformed reference or unknown required field/version | validation error naming the field/version | `fail-fast` | Correct or migrate the reference; retrying unchanged input is not useful. |
| Pre-v3 `missis-ulid-v1` tries to originate a durable v3 reference | `identity-scheme-unsupported` | `fail-fast` + `fail-closed` | Run the #122 identity migration and use its receipt. Inspection remains allowed. |
| No supplied peer claims the requested ID | `unavailable/unknown` | `fail-safe` + `fail-closed` for dependent actions | Supply/recover an authorized peer, then re-resolve the unchanged durable reference. |
| Candidate claim cannot be obtained, times out, or is rate-limited | `unreachable` insight | `fail-safe` | Bounded retry is allowed; cached data remains historical/unverified. |
| Peer denies authorization | `unauthorized` | `fail-closed` | Change explicit authorization policy/credentials; never rewrite as missing. |
| Candidate claims another store ID | `different-store` insight | `fail-closed` for that candidate | Correct peer selection/configuration; entity resolver is not called. |
| Claim is malformed, digest-invalid, expired when expiry is required, or signature-invalid | `invalid-claim` | candidate `fail-stop`; resolver `fail-closed` | Repair/upgrade the peer; do not retry blindly or trust partial fields. |
| Claim uses an unknown digest scheme/integrity epoch or lacks its transition proof | `unsupported-integrity-transition` | `fail-closed` | Upgrade verification support or obtain the named transition proof; do not compare unlike digests as a collision. |
| Same ID has incompatible identity document or genesis | `identity-collision` | `fail-closed` | Operator incident: quarantine candidates and inspect migration/clone provenance. |
| Same identity/genesis has different heads and ancestry is unknown | `divergent-state-unverified` | `fail-closed` | Obtain ancestry/checkpoint proof or declare a writable fork with new identity. |
| Multiple peers prove identical accepted state | `exact-replica` | controlled `fail-over` permitted | Select deterministically under local policy; keep comparison evidence. |
| One peer proves it is behind/ahead on the same ancestry | `stale-replica` / `advanced-replica` | policy-controlled `fail-over` | Use only a peer satisfying the requested checkpoint/freshness boundary. |
| Deferred checkpoint capability is requested but unavailable | `checkpoint-unavailable` | `fail-closed`; optional degraded display | Alpha.3 returns unsupported/degraded. If #58 is later activated, recover the checkpoint authority or use an explicit weaker read policy. |
| Deferred checkpoint capability differs from a pin | `checkpoint-mismatch` | `fail-closed` | Resolve the pinned checkpoint; never refresh the pin automatically. This classification requires #58 proof evidence. |
| Trusted external anchor proves the candidate moved backward | `rollback` | peer `fail-stop`; resolver `fail-closed` | Conditional on #58 implementation: quarantine, compare backup/replica evidence, and require operator recovery. |
| Two valid continuations exist after one trusted boundary | `fork` | `fail-closed` | Conditional on ancestry/#58 proof: apply an explicit fork/authority decision; never choose the longest path implicitly. |
| Namespace, kind, schema, projection, or required capability unsupported | `unsupported` naming the version/capability | `fail-fast` where knowable locally, otherwise `fail-closed` | Upgrade/adapt while retaining exact reference/evidence bytes. |
| Entity genuinely absent in a verified matching authority | `missing` | successful negative resolution | Caller may report missing; it must remain distinct from unavailable/unauthorized. |
| Observation token differs but current resolution succeeds | `stale` | successful resolution; dependent current-state action is closed | Present the change and require explicit re-evaluation; update cache only. |
| Entity is retracted/not yet effective/superseded | corresponding lifecycle | successful resolution; active-only action is closed | Preserve historical pins and apply consumer lifecycle policy. |
| Local peer detects identity, chain, SQLite, or required artifact corruption | integrity-specific diagnostic | peer `fail-stop` | Use non-mutating verification and restore/quarantine workflow; do not serve verified data. |
| Read-only transport outcome is ambiguous | `unavailable` with retryable diagnostic | `fail-safe` | Retry resolution; do not mark cached state current merely because a prior call may have succeeded. |
| Activated reciprocal protocol partially succeeds | `reciprocal-pending` with receipts | `fail-safe` | Conditional blueprint only: retry idempotently or retract explicitly; never claim atomic completion. Alpha.3 performs no such write. |

Resource exhaustion must map to a bounded timeout, size-limit, or rate-limit
classification rather than an unbounded wait or partial parse. Programmer
panics and process termination are implementation defects, not protocol-level
`fail-stop`; recovery still must not reinterpret incomplete output as success.

## 4. Resolution result and lifecycle

Resolution returns structured state rather than “found/not found”:

```text
ResolutionV1 {
  reference
  authority_state = verified | degraded | unavailable | unauthorized
  identity_state = matched | missing | kind-mismatch | identity-collision | unsupported | unknown
  lifecycle = active | retracted | not-yet-effective | superseded | unknown
  freshness = current | stale | unverified
  stream_revision?
  current_event_id?
  verified_checkpoint_digest?
  verified_through_cursor?
  projection_id
  projection_version
  known_at
  effective_at
  evidence_refs[]
  peer_insights[]
  warnings[]
}
```

The current alpha structs implement only the first identity/state subset. The
checkpoint, rollback, fork, retryability, and explicit failure-policy fields
remain schema work; until they land, those conditions MUST return
degraded/unverified and MUST NOT be reported as verified success.

`active` means active under the named temporal projection and times; it never
means “has existed at some point.” A current retraction does not erase pinned
historical evidence. Missing and retracted are distinct. Unsupported schema or
projection versions are `unknown/unsupported`, not missing or corrupt.

The source store retains its `ExternalRefV1` when the target is offline,
unauthorized, missing, stale, or retracted. Resolution state is derived/cache
data and MUST NOT rewrite or delete the accepted source reference.

## 5. Fast stale and retraction detection

Correctness cannot depend on a time-to-live cache. The target authority exposes
a per-stream or per-entity validation token containing at least:

```text
store_id
stream_id or entity_id
stream_revision
current_event_id or lifecycle_event_id
projection_id + projection_version
verified_through_cursor
checkpoint_digest?            when externally verifiable freshness is needed
```

The resolver first performs a conditional validation using the reference's
observation token:

```text
token unchanged -> cached presentation may be reused for that projection/time
token advanced  -> return stale, re-resolve lifecycle, update only the cache
token absent    -> return unverified and perform full resolution when allowed
target offline  -> return unavailable; never report cached active as current
```

Per-entity/stream tokens avoid invalidating every cached reference when an
unrelated ticket changes. A target change feed can proactively invalidate
tokens by `(store_id, stream_id)`, but dropped notifications are harmless
because conditional validation remains authoritative. Polling interval and
cache TTL affect detection latency, not truth; health reports the last
successful validation and cursor lag.

For remote authorities, a stream token is trustworthy only when returned over
an authenticated authority channel and, where rollback resistance is claimed,
bound to a trusted checkpoint from `#58`. A token from the same rollbackable
database detects ordinary changes but cannot prove that the complete store was
not rolled back.

Retraction handling is explicit:

1. retain the foreign reference and its last observation;
2. return `lifecycle=retracted` with the retraction evidence when visible;
3. keep event-pinned evidence resolvable;
4. mark source projections/actions that require an active target as stale or
   invalid according to the consumer schema;
5. never cascade a retraction into another store automatically.

## 6. Forks, copies, and rollback

A backup/restore preserves `store_id`; it is another copy of the same
authority history. A deliberately writable divergent clone MUST receive a new
store/authority identity and record lineage to the origin. Two reachable
writable databases presenting one `store_id` with divergent heads are
`fork-detected`; the resolver must not pick one by path order, modification
time, or label.

Multiple peers may expose replicas for availability only when authority policy
defines how their heads/checkpoints are compared. Without ancestry proof,
same-identity peers with different heads fail closed. External checkpoint
comparison is required to claim rollback detection; local hash-chain
consistency alone does not establish freshness.

## 7. Security and privacy boundary

- Never interpret reference fields as paths or URLs.
- Never follow symlinks, mount paths, redirects, DNS results, or locator hints
  supplied by accepted content. Only configured adapters may do so.
- Remote authorities are allowlisted by local policy; redirects, private
  network access, credentials, TLS identity, and response-size limits are
  adapter responsibilities.
- Resolution is read-only by default and uses the minimum authorized schema.
- Returned display text is untrusted data and is escaped before terminal,
  Markdown, HTML, SQL, or shell use.
- Authorization failure remains `unauthorized`; it is not rewritten as
  `missing`, which would destroy audit meaning.
- Cache entries are partitioned by authority and authorization principal so a
  privileged result cannot leak to a less privileged caller.

## 8. Deferred reciprocal-link reconciliation blueprint

This section documents a dormant design, not alpha.3 behavior. Alpha.3 stores
one-way `ExternalRefV1` values and performs read-only resolution. It does not
contact or mutate the target when a source reference is accepted. The design
below may be activated only by a consumer requirement and a later versioned
contract decision.

### 8.1 Why a reciprocal write is a saga

A reciprocal link means two separate facts:

```text
source store: source entity asserts relation R to target entity
target store: target entity asserts reciprocal relation R' to source entity
```

Each store has its own writer, idempotency table, integrity chain, retention,
authorization, backup, and failure boundary. There is no transaction that can
atomically commit both SQLite databases, especially across machines. A
two-phase commit protocol would add coordinator availability and recovery
requirements without making a failed participant disappear. The proposed
model is therefore a saga: each local append is atomic, every cross-store step
is independently idempotent, and partial progress is a durable visible state.

### 8.2 Proposed durable records

All identities below are canonical store/entity identities and contain no
path, URL, hostname, credential, or peer handle.

```text
CanonicalEndpointV1 {
  store_id
  namespace
  kind
  entity_id
  subentity_id?
}

ReciprocalIntentV1 {
  version = "reciprocal-intent-v1"
  operation_id                  128+ bits CSPRNG, immutable
  source_endpoint
  target_endpoint
  source_relation
  target_relation
  source_assertion_event_id
  source_request_fingerprint
  requested_at
}

ReciprocalTargetReceiptV1 {
  version = "reciprocal-target-receipt-v1"
  operation_id
  source_store_id
  source_intent_event_id
  target_store_id
  target_assertion_event_id
  target_request_fingerprint
  target_stream_revision
  accepted_at
}

ReciprocalCompletionV1 {
  version = "reciprocal-completion-v1"
  operation_id
  source_intent_event_id
  target_receipt_digest
  target_assertion_event_id
  completed_at
}

ReciprocalCancellationV1 {
  version = "reciprocal-cancellation-v1"
  operation_id
  cancelling_store_id
  assertion_event_id
  reason
  requested_at
}
```

The settled contract must publish strict JSON/canonical vectors and define
relation-pair compatibility in the consumer schema. Unknown fields or relation
versions fail before either store is mutated. Display hints and observations
are excluded from operation identity.

### 8.3 Idempotency and local transaction keys

One user action creates one immutable random `operation_id`. Retries reuse it;
they never generate a new operation after an ambiguous response. Each local
step derives a different domain-separated request key:

```text
source_intent_key = SHA256("RECIPROCAL-SOURCE-INTENT" || 0x00 || operation_id)
target_accept_key = SHA256(
  "RECIPROCAL-TARGET-ACCEPT" || 0x00 || source_store_id || 0x00 || operation_id
)
source_complete_key = SHA256("RECIPROCAL-SOURCE-COMPLETE" || 0x00 || operation_id)
target_cancel_key = SHA256(
  "RECIPROCAL-TARGET-CANCEL" || 0x00 || source_store_id || 0x00 || operation_id
)
source_cancel_complete_key = SHA256(
  "RECIPROCAL-SOURCE-CANCEL-COMPLETE" || 0x00 || operation_id
)
```

The full semantic request fingerprint is still checked behind each key. A
same-key/different-operation or changed endpoint/relation is an idempotency
mismatch, not a replay. Two independently initiated logical links have
different operation IDs and remain distinct assertions unless the consumer
schema explicitly enforces uniqueness.

### 8.4 Creation algorithm

1. Resolve and display the target canonical endpoint. Confirm the relation
   pair and target mutation policy. Human aliases are input only.
2. Generate `operation_id` once. In one source-store transaction append the
   source relation/`ExternalRefV1` and `ReciprocalIntentV1`. Return
   `source-pending`; do not wait for the target before making the source fact
   truthful.
3. A reconciler reads pending source intents. It obtains a separately
   authorized **mutation** authority; read-only peer authorization is
   insufficient. It sends the exact intent event and source authority proof,
   bounded by operation ID and endpoint identities.
4. The target validates authentication/authorization, source identity and
   intent proof, its own target identity, consumer schema/relation pair,
   lifecycle/preconditions, request bounds, and the deterministic target key.
   It never accepts a target locator supplied by the intent.
5. In one target-store transaction append the reciprocal assertion and
   `ReciprocalTargetReceiptV1`. If the same request already committed, return
   the original receipt. If the response is lost, retrying the same key finds
   the same target event rather than adding a duplicate.
6. The source verifies the target receipt, target identity, operation ID,
   intent event ID, relation, and—when required—the remote authority/checkpoint
   evidence. In one source transaction append `ReciprocalCompletionV1` using
   `source_complete_key`.
7. The source now derives `complete`. The target derives `accepted` from its
   own assertion/receipt. No final target acknowledgement is required: adding
   acknowledgement-of-acknowledgement rounds cannot create atomicity and would
   only move the last ambiguous message.

The source intent is the reconciliation work queue of record. An in-memory or
external queue may accelerate delivery but losing it cannot lose the pending
operation.

### 8.5 Derived state machine

| Durable evidence | Derived state | Allowed next action |
| --- | --- | --- |
| No source intent | `absent` | Start a new operation. |
| Source intent active; no verified target receipt | `source-pending` | Retry target accept or explicitly cancel. |
| Target receipt exists; source completion absent | `target-accepted-awaiting-source-receipt` | Fetch/verify the idempotent target receipt and append source completion. |
| Source completion matches target receipt | `complete` | Read-only validation; no further write required. |
| Target denies policy/schema/precondition | `target-denied` | Preserve source intent and exact denial; operator may cancel or correct policy and retry when safe. |
| Same operation has conflicting endpoints/relations/receipts | `reciprocal-conflict` | Fail closed; quarantine reconciliation for operator review. |
| Source cancellation active; target cancellation unconfirmed | `cancel-pending` | Retry target cancellation. Do not call the target assertion retracted. |
| Target retraction receipt verified | `cancelled` | Append/return source cancellation completion idempotently. |
| Target independently retracts its assertion | `target-retracted` | Report the divergence; never recreate it automatically. |
| Either authority unavailable | retain prior state plus `unavailable` | Retry with backoff; never infer success or absence. |

Status is folded from immutable local records and verified foreign receipts,
not from a mutable boolean. Backup/restore or process restart therefore
reconstructs the same state. A rolled-back store may re-expose old pending
work; checkpoint protection, if configured, detects the rollback rather than
letting the reconciler guess.

### 8.6 Cancellation and retraction algorithm

1. Retraction of the source relation appends a source cancellation intent; it
   does not directly claim that target state changed.
2. If the target assertion never committed, target cancellation records an
   idempotent `not-created` receipt for that operation.
3. If it committed and remains active, the target retracts the exact assertion
   event—not every semantically similar relation—and records the cancellation
   receipt in the same transaction.
4. If it was already retracted, the target returns the existing compatible
   receipt. A different assertion or cancellation cause is a conflict.
5. The source verifies and records cancellation completion. Until then the
   state remains `cancel-pending`.

Target-initiated retraction is not automatically mirrored. The source learns
it through ordinary resolution/reconciliation and reports `target-retracted`;
consumer policy decides whether a human should retract the source assertion.
Automatic cascades remain prohibited.

### 8.7 Security and resource boundaries

- Activating this blueprint depends on authenticated mutation authority under
  `#48`; `sqlite-live-readonly-v1` can never be upgraded implicitly to a writer.
- Checkpoint proof under `#58` is required only when policy claims rollback
  resistance; otherwise receipts are authenticated current observations, not
  newest-state proof.
- Each request is bounded in bytes, events, retries, and deadline. Retry uses
  persisted exponential-backoff metadata or an equivalent scheduler, never a
  blocking database transaction.
- Credentials/endpoints are operator transport configuration. Accepted intents
  and receipts contain canonical identities and evidence digests only.
- Authorization denial is retained as `target-denied`, distinct from missing
  or unavailable, and is not leaked to unauthorized readers.

### 8.8 Crash/fault conformance blueprint

Hermetic tests use two temporary stores and an in-process authenticated fake
transport. Inject failure before and after source intent commit, request send,
target append, target receipt commit, response delivery, source completion,
source cancellation, target retraction, and cancellation receipt delivery.
Also inject duplicate, delayed, reordered, and corrupted messages; changed
request under the same key; target denial; wrong store/principal; stream
precondition conflict; both stores restarting at every state; response loss
after either commit; independent target retraction; and rollback/fork evidence.

For every boundary, assert that each store contains either its prior valid
state or one complete local transaction; retry converges without duplicate
assertions; a partial operation remains visibly pending; no path/credential is
accepted from content; and no result claims cross-store atomic completion.

Human syntax such as `spy-testing#42` remains resolver input only. Repository
movement changes peer configuration, not endpoints, intents, or receipts.

## 9. Conformance matrix

Core alpha.3 rows are required fixtures/tests. Rows explicitly marked
conditional become required only if their deferred capability is activated by
a later versioned contract; they do not reopen completed #120.

| Case | Required result |
| --- | --- |
| Two stores both contain `#42` | Canonical store/entity IDs resolve without collision. |
| Repository/store moved | Reconstructing an authorized peer handle restores navigation; no accepted event changes. |
| Malicious path/URI in display text | It is inert and escaped; no file/network access occurs. |
| Candidate exposes another store | `different-store` insight includes expected/claimed IDs and claim digests; do not resolve by alias. |
| Same ID with different identity/genesis evidence | `identity-collision`; no peer selected. |
| Same identity with different unproven heads | `divergent-state-unverified`; no peer selected. |
| Target offline with cached active state | `unavailable`, cached state labelled historical/unverified. |
| Target stream advances unrelated entity | Referenced entity token remains current. |
| Referenced entity changes | `stale`, then new lifecycle/current event after re-resolution. |
| Target retracted | Entity reference reports retracted; event pin still retrieves historical evidence. |
| Conditional #58: checkpoint mismatch/rollback | `checkpoint-mismatch` or `rollback` according to trusted anchor evidence. |
| Divergent writable copies share store ID | `fork-detected`; no arbitrary winner. |
| Unknown schema/projection | `unsupported`; exact reference/evidence remains preserved. |
| Conditional reciprocal protocol: target write fails | Source is visibly `reciprocal-pending`; retry is idempotent. |

## 10. Hill-climbing implementation sequence

### 10.1 Implemented local alpha slice

The implemented slice is intentionally smaller than the complete resolver
contract and is the prerequisite for the `event-tooling` repository decision
in `#121`. It provides:

1. public `ExternalRefV1`, pin, observation, query, resolution, and stable
   state types;
2. strict JSON parsing that rejects unknown fields, so a path/URI/locator
   cannot be smuggled into accepted reference data;
3. a canonical identity key that excludes display and observation fields;
4. a concurrency-safe in-memory set of already-authorized peer handles with
   no `store_id` registration or filesystem API;
5. a fresh identity/state claim from every candidate on every resolution;
6. a Missis authority adapter for canonical ticket and Part identity using an
   already-open service handle;
7. per-stream revision/current-event comparison that reports `stale` after a
   referenced stream changes; and
8. explicit `unavailable`, `different-store`, `identity-collision`,
   `divergent-state-unverified`, `missing`, `active`, `retracted`, and
   `unsupported` outcomes; and
9. a strict format-5 `external-ref` value kind accepted by SDK and CLI data
   writes, restored as a typed value after close/open and backup, retained when
   unresolved, and rendered with explicit unresolved identity;
10. one verified authority snapshot for claim and event-fold resolution, with
    the prior two-call API removed;
11. strict bounded `missis-local-peer-set-v1` configuration and
    `sqlite-live-readonly-v1` Linux peers; and
12. `missis-tools peers inspect` plus opt-in `missis show ...
    --resolve-external --peer-config FILE` navigation.

This slice persists an external **value** in the Missis ledger and constructs a
local read-only peer only from explicit operator configuration. It does not
implement or activate the deferred reciprocal saga blueprint, resolve a remote
authority, verify checkpoints, subscribe to a change feed, or create an
authority handle from accepted data. Domain/reference code remains
filesystem-neutral; only the local adapter sees `sqlite_path`.

First-slice conformance evidence is:

| Case | Expected result |
| --- | --- |
| Unknown JSON field such as `locator` | Parse rejection; no resolution attempt. |
| Same identity, changed display/observation | Same canonical identity key. |
| No supplied peer claims store ID | `authority_state=unavailable`, reference retained, no global-registration claim. |
| Candidate claims another store ID | `different-store` peer insight carries expected/claimed fields; entity resolver is not called. |
| Same ID, incompatible genesis | `identity-collision`; entity resolver is not called. |
| Same identity, divergent head/count | `divergent-state-unverified`; entity resolver is not called. |
| Two stores allocate the same human alias | `store_id` selects the intended canonical entity. |
| Observation matches stream token | `freshness=current`. |
| Target stream advances | `freshness=stale` with new revision/current event. |
| Part value is retracted | `lifecycle=retracted`; identity remains matched. |
| Checkpoint pin before checkpoint support | Explicit degraded/unsupported result, never false verification. |

### 10.2 Implemented and remaining algorithms with hermetic gates

| Slice | Algorithm boundary | Required hermetic evidence before implementation is called complete |
| --- | --- | --- |
| **Confirmed:** opaque local peer construction | Strict `missis-local-peer-set-v1` configuration supplies an opaque handle, mandatory expected store ID, `sqlite-live-readonly-v1`, and adapter-private absolute path. The adapter opens an existing shared lease without creating it, uses SQLite `mode=ro`/`query_only`, verifies format, identity, and chain, then claims and folds the requested entity in one read snapshot. Stored content supplies no path and domain code receives no path. | Sections 3.2 and 3.2.1 cover moved store; unchanged database/accepted-WAL/schema/projection/artifact/lock content; only documented WAL/SHM sidecars; coherent concurrent append; projection drift; wrong store with detailed claim comparison; exact/divergent replica; malicious locator rejection; lifecycle after reopen; and old/corrupt store. |
| Conditional stream validation | Ask the matching peer for `(store_id, stream/entity ID, revision, current/lifecycle event, projection version, verified cursor)` on every resolution. Compare the stored observation; unchanged is current, advanced is stale, absent is unverified. | Advance unrelated and referenced streams separately; only the referenced stream becomes stale. Drop all cache/change notifications and prove conditional validation still detects it. Close/reopen both peers and repeat. |
| Change-feed invalidation | Subscribe by `(store_id, stream/entity ID)` and evict only matching cached observations. It is an optimization; lost/duplicated/out-of-order notifications never establish freshness. | Deterministic fake feed injects loss, duplication, reordering, cursor expiry, reconnect, and unrelated changes; subsequent conditional validation produces the same result as a no-cache resolver. |
| **Skipped for alpha.3:** checkpoint proof (`#58`) | A checkpoint would be an independently trusted, signed statement binding `store_id`, head/checkpoint digest, event count or cursor, integrity epoch, and signing authority. It is needed to detect whole-store rollback or to prove which divergent head descends from another; the local hash chain alone cannot detect a complete internally consistent rollback. Alpha.3 preserves a checkpoint pin but returns explicit unsupported/degraded state and grants no rollback assurance. | If resumed under #58, fixed-key hermetic tests must cover valid proof, unknown key, bad signature, wrong store, wrong epoch, missing transition, pin mismatch, rollback, fork, expiry, and replay. No checkpoint implementation is required by #115 or #121. |
| **Skipped for alpha.3:** authenticated remote peer (`#48`) | A remote adapter would bind a transport principal and authorized store ID/capabilities, then verify a bounded signed claim. Store identity alone proves document binding, not who controls a network endpoint. Alpha.3 accepts no URL, hostname, credential, redirect, or remote authority configuration. | If resumed under #48, an in-process authenticated transport must cover wrong principal, expired/rotated key, wrong store, redirect, denial, timeout, oversized response, replay, exact replica, and divergent peer. It is not required by #115 or #121. |
| **Non-goal for alpha.3:** reciprocal-link reconciliation | A reciprocal relation would require separate mutations in source and target stores. No SQLite or network transaction can atomically commit both. A future design would need independently idempotent source/target intents, receipts, visible `reciprocal-pending` partial state, retry, and explicit one-sided retraction. Alpha.3 performs no target mutation and promises no backlink. | Revisit only if a consumer requires two-way relation materialization. Any implementation must fault at every append/receipt boundary and prove restart/retry behavior without claiming cross-store atomicity. It is not required by #115 or #121. |

1. **Confirmed:** freeze strict `ExternalRefV1` JSON/identity vector and the
   format-5 durable fixture.
2. **Confirmed:** implement the initial in-memory resolver with caller-supplied
   already-open SQLite authorities; alpha.2 then replaced its two-call
   authority boundary with the coherent snapshot API in step 6.
3. **Confirmed:** add identity claims and structured
   unavailable/collision/divergence insight.
4. **Confirmed:** add per-stream revision/current-event comparison during full
   resolution.
5. **Confirmed:** add durable unresolved values and Missis CLI/SDK display.
6. **Confirmed:** add the section 3.2 snapshot API, strict local peer-set
   codec, read-only SQLite authority, protected `peers inspect` command, and
   opt-in `show` resolution integration. Conditional validation remains the
   correctness fallback; change-feed invalidation follows only as an
   optimization.
7. **Deferred outside this ticket:** authenticated remote authorities and
   trusted checkpoint proofs may resume only under `#48` and `#58`; they are
   not prerequisites for artifact namespace work or repository restructuring.
8. **Explicit alpha.3 non-goal:** reciprocal-link reconciliation. A durable
   external reference is one-way evidence/navigation and never implies a
   target-store write, backlink, distributed transaction, or cascade.

## 11. Confirmed, unknown, and deferred

Confirmed: aliases/paths are insufficient identity; `store_id` is required;
offline references remain durable; cross-store SQLite constraints do not
exist. Format 5 implements strict durable values, path-free supplied peer
handles, strict operator-configured `sqlite-live-readonly-v1` peers on Linux,
one-snapshot identity/entity comparison, protected inspection, moved-store
navigation, self-certifying `eventstore-hash-v1` claims, ticket/Part
resolution, and stale/retracted classification. Format
4 and older stores cannot open normally or originate new durable references;
they require the exact format-5 migration and receipt.

Confirmed limitation and deliberate skip: native Windows, WSL/DrvFS, and
network-filesystem local peers are unsupported under #112; sealed-snapshot
peers are not implemented; authenticated remote transport is deferred to #48;
checkpoint and rollback proof is deferred to #58. Checkpoint pins therefore
return degraded/unsupported, and divergent heads cannot be classified as
stale/advanced without ancestry proof. None of these is a gate for #115 or
#121.

Explicit non-goals for alpha.3 are reciprocal target-store writes,
cross-store atomic commits, automatic cascades, transparent remote mutation,
multi-authority merge, and global alias allocation. A future consumer need
must open a new versioned decision before any of these enter the protocol.
