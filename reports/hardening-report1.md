# Missis Detailed Engineering Review and Hardening Report

> **Transient:** generated analysis artifact, not an authoritative contract.
> It is review context; the authoritative specs and store are listed in
> AGENTS.md "Document authority".

**Repository:** `ravinsharma7/missis`  
**Reviewed commit:** `da34b8ae0ad740a7628d2f087f0a44eba00bd532`  
**Review date:** 2026-08-17  
**Review type:** Static architecture, implementation, contract, safety, and product review  
**Project status at review:** Alpha

---

## 1. Purpose of this report

This report expands the earlier Missis project review into an implementation-grade engineering assessment.

It is intended to answer four questions:

1. **What is already strong and worth preserving?**
2. **Which current behaviors can violate the intended Missis model?**
3. **What exact contracts, algorithms, tests, and migrations should be added?**
4. **In what order should the work be completed so the project improves by hill climbing rather than by a rewrite?**

The review concentrates on the temporal provenance kernel because everything planned later—ontology, processors, code provenance, hybrid search, replication, and agent automation—depends on that kernel being deterministic and trustworthy.

---

## 2. Review scope and limitations

### 2.1 Material inspected

The review inspected the current repository structure and relevant source at commit:

```text
da34b8ae0ad740a7628d2f087f0a44eba00bd532
```

The main inspected areas were:

```text
README.md
specs/
docs/
cmd/missis/main.go
pkg/missis/
implementation/model/
implementation/store/
implementation/store/migrations/
testsuite/blackbox/
testsuite/benchmarks/
tools/
```

Specific functions and contracts discussed in this report include:

```text
store.Open
store.ensureStoreIdentityAndHashes
store.rebuildHashesTx
store.CheckConsistency
store.RepairSequenceGaps
store.appendBatchOnce
store.LoadEvents
store.ListTickets

model.ProjectTicket
model.sortEvents
model.applyEvent
model.ValidateAppend
model.validOperation
model.LinksForRef
model.BuildLineageGraph
model.LineageGraph.Walk
model.refEqual
model.refKey
model.ParseMarkdownParts

missis.Client.NewTicket
missis.Client.ShowTicket
missis.Client.SetPart
missis.Client.Search
missis.ResolveStorePath
```

### 2.2 What was not confirmed

I did not execute the repository test suite in this review environment because a local checkout could not be fetched from GitHub from the execution container.

Therefore:

```text
confirmed:
    source structure and current implementation behavior visible in repository

not confirmed:
    all tests pass at the reviewed commit
    runtime performance measurements on a real store
    platform-specific behavior on Windows/macOS/Linux
```

All implementation claims below are based on the reviewed source. Where a conclusion is an inference, it is labelled as such.

---

## 3. Executive assessment

Missis has a valuable and coherent core idea:

```text
simple command surface
        +
immutable event model
        +
recursive addressable parts
        +
typed links
        +
temporal projections
        +
provenance-first operation
```

The three-domain-command contract is especially strong:

```text
missis new
missis show
missis set
```

The implementation already contains more substance than a typical early prototype:

- an event ledger;
- stable entity identifiers;
- recursive parts;
- valid-time and bitemporal projections;
- retraction and supersession;
- typed links and inverse links;
- lineage traversal;
- Markdown import/export and round trips;
- projects and groups;
- optimistic concurrency;
- idempotency;
- SQLite backup and repair tools;
- a public Go facade;
- portable black-box tests;
- fuzzing and concurrency tests;
- agent onboarding and benchmark tooling;
- a two-way machine-readable requirements registry added at the reviewed commit.

The main concern is not that the project lacks features. The main concern is that several foundational semantics are still ambiguous or internally inconsistent.

The strongest recommendation is:

> Freeze and harden identity, integrity, temporal ordering, operation semantics, and projection behavior before adding runtime ontology, plugins, vector search, or peer-to-peer synchronization.

---

## 4. Priority overview

| Priority | Finding | Why it matters |
|---|---|---|
| **P0** | Hash chain is rebuilt during normal open | Can hide prior alteration and makes every open increasingly expensive |
| **P0** | Repair rewrites accepted events | Conflicts with append-only provenance and can invalidate external references |
| **P0** | Bitemporal winner rule is not fully defined | Different correct-looking implementations can produce different state |
| **P1** | Link graph keys include mutable paths | Rename or move can split one canonical part into multiple graph nodes |
| **P1** | Some accepted operations have no explicit projection semantics | Durable events can be accepted without a meaningful observable effect |
| **P1** | Canonical event encoding is Go-serialization-dependent | Cleanroom implementations may calculate different hashes |
| **P1** | CLI and SDK do not yet share one complete application layer | Behavior can drift between CLI, TUI, SDK, and future APIs |
| **P1** | Store discovery is a trust boundary | A cloned repository can influence where data is read or written |
| **P1** | Local-authoritative versus mergeable storage is undecided | Current sequence/hash design does not naturally support offline merges |
| **P2** | Markdown duplicate headings can collide | Repeated headings can produce ambiguous or duplicate paths |
| **P2** | Markdown preamble can be discarded | Ingestion may silently lose user content |
| **P2** | Lineage traversal returns a spanning tree, not necessarily the full graph | Converging evidence and cyclic provenance can disappear from output |
| **P2** | Projection and append paths repeatedly scan/fold events | Cost will grow before advanced search becomes useful |
| **P2** | Search returns tickets rather than canonical part hits | Limits precise agent retrieval and hybrid search composition |
| **P2** | Repository protections lag behind code quality | Strong tests are not enforced at the merge boundary |
| **Strategic** | Product positioning overstates simplicity of the whole model | The interface is simple; the semantic model is deliberately sophisticated |

The priorities mean:

```text
P0:
    settle before a durable store format is declared stable

P1:
    settle before public alpha/beta usage or plugin/ontology execution

P2:
    important, but can follow the semantic kernel hardening

Strategic:
    affects expectations, documentation, and adoption
```

---

# 5. Finding 1 — Hash-chain lifecycle is unsafe and unnecessarily expensive

## 5.1 Verdict

**Confirmed and high priority.**

Normal store opening currently initializes the store identity and then rebuilds the complete event hash chain.

The relevant path is:

```text
store.Open
    ↓
migrate
    ↓
ensureStoreIdentityAndHashes
    ↓
loadEventsTx
    ↓
rebuildHashesTx
    ↓
DELETE FROM event_hashes
    ↓
recalculate all hashes
    ↓
replace store_meta.head_hash
```

The current implementation does not merely verify existing integrity metadata. It replaces that metadata with hashes derived from the current event rows.

## 5.2 Why the current behavior is problematic

### Performance property

Let:

```text
N = total accepted event count
```

Then a normal open performs work approximately proportional to:

```text
T_open(N) ∈ O(N)
```

It also takes a write transaction because the hash table and head are rewritten.

Since most CLI commands open the store, the cost becomes:

```text
command latency
    =
CLI work
    +
full hash rebuild
```

Even a read such as:

```bash
missis show '#12'
```

can require an operation over all stored events before the read begins.

Consequences include:

- startup latency grows with the complete history;
- read-only operations take a writer lock during opening;
- concurrent readers can be delayed by hash maintenance;
- startup cost is paid repeatedly instead of only when needed;
- benchmark results for `show` can become dominated by store opening rather than projection.

### Integrity property

Suppose the accepted event is:

```text
E
```

An external process changes the row to:

```text
E'
```

Normal opening then computes:

```text
H' = Hash(E')
```

and stores `H'` as the new head.

After that, consistency checking can report that the current rows and the current head agree.

Therefore the current mechanism proves:

```text
hash metadata is consistent with event rows after normal opening
```

It does not prove:

```text
event rows are unchanged from the state previously accepted by Missis
```

Formally:

```text
Rebuild(events_original) = valid chain
Rebuild(events_modified) = valid chain
```

A rebuild is a normalization procedure, not a historical integrity verification procedure.

## 5.3 Secondary issue: intermediate hash rows are not fully verified

`CheckConsistency` currently checks:

- stream sequence continuity;
- JSON validity of idempotency event-ID lists;
- the number of rows in `event_hashes`;
- a newly recomputed final head against `store_meta.head_hash`.

It does not appear to compare every stored `event_hashes.hash` and `previous_hash` value with the recomputed values.

This distinction matters:

```text
trusted head + recomputation of all events
    can be sufficient to verify the event sequence

but

event_hashes table correctness
    is not proved merely by counting its rows
```

If `event_hashes` is intended only as an acceleration table, it should be declared rebuildable and non-authoritative.

If it is intended as an auditable per-event chain, each row should be verified.

## 5.4 Threat-model matrix

The project should explicitly choose which threats the hash system addresses.

| Threat | Example | Local head inside same DB sufficient? | External anchor needed? |
|---|---|---:|---:|
| Accidental corruption | Bit flip, truncated JSON, partial manual edit | Usually | Helpful |
| Implementation inconsistency | Wrong sequence or hash generated by a bug | Usually | Not always |
| Offline event alteration without updating metadata | Event row changed, head left untouched | Yes, if normal open verifies rather than rebuilds | No |
| Offline alteration of both event and metadata | Attacker edits event rows and stored head | No | Yes |
| Running process with DB write authority is malicious | Process appends or rewrites plausible history | No | Yes, plus authorization |
| Storage rollback | Entire DB replaced by an older valid copy | No | Yes, monotonic external checkpoint |
| Fork substitution | Different valid store supplied at same path | Store ID helps but is not enough alone | Usually |

A precise claim could be:

```text
Version 1 goal:
    detect accidental corruption,
    inconsistent sequence/hash state,
    and event alteration where integrity metadata was not also replaced.

Non-goal:
    defend against an attacker who controls the database and every trust anchor.
```

That is a credible initial contract.

## 5.5 Required contracts

### Normal open

**Preconditions**

```text
path resolves to an allowed store location
schema can be read or migrated
store identity is present or this is first initialization
```

**Postconditions**

```text
no accepted event row was modified
no existing hash row was replaced
no head hash was replaced
store is returned only if the configured verification policy succeeds
```

Pseudocode:

```text
Open(store):
    migrate_if_required()

    if store_is_new:
        initialize_identity_and_empty_head()
    else:
        verify_integrity()

    return handle
```

### Normal append

**Preconditions**

```text
current stored head is verified or trusted for this transaction
new batch passes validation and preconditions
allocated stream sequence is contiguous
```

**Postconditions**

```text
events are appended atomically
hash chain is extended atomically
head is advanced atomically
idempotency result is stored atomically
existing event bytes remain unchanged
```

### Explicit repair

Repair must be an exceptional operation.

**Preconditions**

```text
operator explicitly requests repair
backup exists or operator waives backup
repair plan is shown in dry-run mode
repair policy is selected
```

**Postconditions**

```text
repair does not masquerade as normal history
every mutation is recorded in a repair report
old and new store identities/heads are preserved
repair cannot silently occur during ordinary Open()
```

## 5.6 Recommended architecture

Separate these concepts:

```text
InitializeIntegrity
VerifyIntegrity
ExtendIntegrity
RepairIntegrity
AnchorIntegrity
```

Do not reuse one rebuild function for all five.

Suggested API:

```go
type IntegrityMode int

const (
    IntegrityVerifyFast IntegrityMode = iota
    IntegrityVerifyFull
    IntegritySkip // explicit unsafe/debug use only
)

type OpenOptions struct {
    Path          string
    IntegrityMode IntegrityMode
    ReadOnly      bool
}

func Open(opts OpenOptions) (*Store, error)

func InitializeIntegrity(tx *sql.Tx) error
func VerifyIntegrity(ctx context.Context, q Queryer, mode IntegrityMode) error
func ExtendIntegrity(tx *sql.Tx, appended []model.Event) error
func BuildRepairPlan(ctx context.Context, q Queryer) (RepairPlan, error)
func ApplyRepair(ctx context.Context, db *sql.DB, plan RepairPlan) (RepairReceipt, error)
```

### Fast verification

A fast path may verify:

```text
schema version
store ID
event count
last alias sequence
last event ID
stored head presence
stream-head constraints
```

This is useful for ordinary reads but does not replace periodic full verification.

### Full verification

A full verification should:

1. read one consistent snapshot;
2. verify every event can be decoded;
3. verify canonical event bytes;
4. verify stream sequence rules;
5. recompute every chain step;
6. compare each stored hash row if retained;
7. compare final head;
8. verify optional external checkpoint;
9. return a structured report.

## 5.7 External anchoring options

An external anchor prevents the database from silently rewriting its own history.

Possible levels:

### Level 0 — no external anchor

```text
store DB contains event rows + head
```

Protects mainly against uncoordinated corruption.

### Level 1 — local signed manifest

```text
manifest:
    store_id
    head_hash
    event_count
    timestamp
    signer_key_id
    signature
```

The signing key must not be writable by every process that can modify the DB.

### Level 2 — append-only remote checkpoint

Periodically upload:

```text
(store_id, head_hash, event_count, recorded_at)
```

to an object store using immutable/versioned objects.

### Level 3 — replicated witness

One or more independent systems confirm:

```text
Witness(store_id, head_hash, count, time)
```

Missis does not need Level 3 initially. The design should leave room for it.

## 5.8 Migration plan

A safe migration from the current behavior:

### Step 1

Introduce:

```text
integrity_format_version
integrity_initialized_at
integrity_verified_at
```

in `store_meta`.

### Step 2

On first open after migration:

```text
if integrity_format_version is absent:
    require explicit one-time initialization
    or perform migration initialization with a visible receipt
```

Do not present migration initialization as evidence that earlier history was untampered. It only establishes a trust baseline from that moment.

### Step 3

Stop deleting and rebuilding `event_hashes` in normal `Open`.

### Step 4

Make normal append extend the chain only.

### Step 5

Move complete rebuild to an explicit maintenance command or tool.

### Step 6

Add optional external checkpointing.

## 5.9 Test plan

### Corruption tests

- modify one event byte and leave head unchanged;
- remove one event;
- duplicate one event;
- reorder alias sequence values;
- alter a stream sequence;
- alter only the head;
- alter only one intermediate hash row;
- remove one hash row;
- add an unrelated hash row;
- replace the database with an older copy.

Expected default:

```text
Open → integrity failure
No automatic repair
No metadata rewrite
```

### Crash tests

Inject failure:

- after event insertion but before hash insertion;
- after hash insertion but before head update;
- after head update but before commit;
- during checkpoint creation.

Expected:

```text
transaction rollback leaves previous valid state
or
recovery detects incomplete state and fails closed
```

### Performance tests

Measure:

```text
open with 0, 1k, 10k, 100k, 1M events
read-only show latency
concurrent show latency
append latency
full verify latency
```

Normal open should not grow linearly with total history unless full verification is explicitly requested.

## 5.10 Acceptance criteria

- [ ] Normal `Open` never deletes or rewrites integrity metadata.
- [ ] Normal `Open` never changes accepted event bytes.
- [ ] Tampering with an event causes open or health verification to fail.
- [ ] A full verification produces a deterministic structured report.
- [ ] Normal append extends the existing head atomically.
- [ ] Explicit repair requires an explicit command and produces a receipt.
- [ ] Integrity initialization is distinguishable from integrity verification.
- [ ] The threat model is documented.
- [ ] Benchmarks show normal open is no longer `O(total events)`.

---

# 6. Additional P0 finding — Sequence repair rewrites accepted historical events

## 6.1 Verdict

**Confirmed and inconsistent with a strict append-only ledger.**

`RepairSequenceGaps` directly executes an update similar to:

```sql
UPDATE events
SET sequence = ?, event_json = ?
WHERE id = ?
```

It then deletes and rebuilds integrity hashes.

This means the event identified by a stable event ID can have different bytes before and after repair.

## 6.2 Why this matters

Missis states that accepted history is immutable.

The intended invariant is:

```text
Accepted(e, t1)
    →
∀t2 > t1:
    EventBytes(e, t2) = EventBytes(e, t1)
```

The current repair behavior permits:

```text
EventID(e) remains constant
∧
EventBytes_before(e) ≠ EventBytes_after(e)
```

This has several consequences:

- exact event references no longer identify exact immutable content;
- an external signature over an event becomes invalid;
- an exported provenance proof can no longer be reproduced;
- cached projections keyed by event ID can become stale;
- a remote checkpoint can disagree with the repaired local store;
- “repair” can conceal whether a gap was original corruption or a later rewrite.

## 6.3 Better repair choices

There are three legitimate repair strategies.

### Strategy A — fail closed and restore backup

Preferred for strict provenance:

```text
sequence gap detected
    →
store considered corrupted
    →
restore a known-good backup
```

### Strategy B — create a new repaired store

Preserve the original store as evidence.

```text
original store:
    immutable, marked damaged

new store:
    new store_id
    imports recoverable events
    records source store and repair mapping
```

The repair receipt should map:

```text
old event ID
old bytes hash
old sequence
new event ID or imported event ID
new sequence
reason
```

### Strategy C — append compensating metadata without renumbering

If gaps are harmless, redefine the sequence invariant:

```text
strictly increasing
```

instead of:

```text
contiguous from 1
```

Then gaps do not require rewriting accepted events.

This may be the cleanest choice.

Ask:

> Does Missis actually require contiguous sequence values, or only a total deterministic order within a stream?

For ordering, this is enough:

```text
∀e1,e2 in stream:
    e1 ≠ e2 → sequence(e1) ≠ sequence(e2)
```

and:

```text
sequence is monotonic for newly accepted events
```

Contiguity is useful for detecting missing data, but a detected gap should be evidence of missing data, not something automatically erased by renumbering.

## 6.4 Recommended decision

Use:

```text
sequence:
    immutable, unique, monotonically increasing
```

Treat a gap as an integrity incident.

Do not renumber accepted events in place.

## 6.5 Acceptance criteria

- [ ] No maintenance path updates `events.event_json` for accepted events.
- [ ] No maintenance path changes an accepted event sequence.
- [ ] Gap detection and gap repair are separate.
- [ ] A repair creates a new store or restores from backup.
- [ ] Any repair has a provenance-bearing receipt.
- [ ] Exact event references remain exact for the lifetime of a store.

---

# 7. Finding 2 — Canonical identity is not applied consistently in links

## 7.1 Verdict

**Confirmed.**

The model correctly separates:

```text
Ref.Kind
Ref.Entity  // canonical immutable identity
Ref.Path    // optional human-readable alias
```

However, link code currently uses two identity functions:

```text
refEqual(a, b):
    Kind + Entity
```

and:

```text
refKey(ref):
    Kind + Entity + Path
```

The link maps and lineage adjacency maps use the path-sensitive key.

## 7.2 Failure scenario

Initial state:

```text
PartID:
    part:ABC

Current path:
    #12/evidence/race-test
```

A link is asserted:

```text
part:ABC --supports--> part:XYZ
```

The stored reference also carries the old path.

Later, the part moves:

```text
#12/evidence/race-test
    →
#12/verification/race-test
```

Canonical identity is unchanged:

```text
Identity(old alias) = Identity(new alias) = part:ABC
```

But graph keys may differ:

```text
part:part:ABC:evidence/race-test
part:part:ABC:verification/race-test
```

Possible outcomes:

- lineage queried using the new path does not enter the adjacency list created using the old path;
- retraction generated from the new path creates a different map key;
- old and new aliases appear as separate nodes;
- sorting and deduplication depend on mutable display metadata;
- a rename changes graph behavior even though no link changed.

## 7.3 Required distinction

Define two separate functions.

```go
func CanonicalRefKey(ref Ref) string {
    return string(ref.Kind) + "\x00" + ref.Entity
}

func PresentationRefKey(ref Ref) string {
    return CanonicalRefKey(ref) + "\x00" + strings.Join(ref.Path, "/")
}
```

Use the canonical key for:

- equality;
- link identity;
- adjacency;
- deduplication;
- retraction matching;
- provenance targets;
- graph traversal;
- index keys.

Use the presentation key only for:

- display sorting where desired;
- path lookup;
- diagnostics;
- historical alias views;
- source-location provenance.

## 7.4 Link identity contract

A link should have an identity independent of display aliases:

```text
LinkIdentity =
    CanonicalFrom
    × RelationID
    × CanonicalTo
    × AssertionIdentity
```

There are two useful link models.

### Set semantics

At most one active assertion exists for:

```text
(from, relation, to)
```

Multiple identical assertions deduplicate.

### Evidence semantics

Multiple actors/processors may independently assert the same relation:

```text
assertion A by human
assertion B by plugin
assertion C imported from external source
```

The visible relation is active while at least one non-retracted assertion remains.

For provenance-first Missis, evidence semantics are more expressive.

That suggests:

```text
LinkAssertion {
    id
    from
    relation
    to
    actor
    source
    created_by_event
    retracted_by_event?
}
```

Then the relation projection is:

```text
ActiveRelation(from, relation, to)
    ⇔
∃ assertion:
    active(assertion)
```

## 7.5 Migration impact

Existing event records do not need to be rewritten.

The correction can be projection-only:

```text
old events retain old path aliases
canonical identity uses Kind + Entity
current display path is resolved from current projection
```

Search and graph indexes must be rebuilt after changing the key definition.

## 7.6 Tests

### Rename survival

1. Create parts `P` and `Q`.
2. Assert `P supports Q`.
3. Rename `P`.
4. Query references using the new path.
5. Confirm the relation remains.
6. Query the historical event and confirm it retains the old path provenance.
7. Retract through the new path.
8. Confirm no active relation remains.

### Move survival

Repeat using parent movement.

### Alias collision

1. Move old `P` away.
2. Create new part at the old path.
3. Confirm the old link remains attached to old `PartID`, not the new occupant.

This test is crucial:

```text
same path at different time
    ≠
same canonical part
```

### Diamond and inverse tests

Confirm inverse projection uses canonical identity and does not duplicate nodes after moves.

## 7.7 Acceptance criteria

- [ ] One canonical reference-key function exists.
- [ ] Mutable paths never participate in graph identity.
- [ ] Link retraction succeeds after source or target rename/move.
- [ ] Reusing an old path for a new part never transfers old links.
- [ ] Current display resolves canonical IDs to current paths.
- [ ] Historical views can still display historical paths.
- [ ] Link indexes can be rebuilt without rewriting source events.

---

# 8. Finding 3 — The bitemporal winner rule must be formalized

## 8.1 Verdict

**Confirmed as a semantic ambiguity, not necessarily a coding bug.**

The current projector filters events by:

```text
event.effective_at ≤ requested_effective_time
∧
event.recorded_at ≤ requested_known_time
```

It then sorts primarily by stream sequence and folds events in that order.

This defines a behavior, but the intended meaning has not been made sufficiently explicit for cleanroom implementations.

## 8.2 Example

```text
e1:
    sequence = 1
    recorded = 10:00
    effective = 10:00
    status = open

e2:
    sequence = 2
    recorded = 12:00
    effective = 12:00
    status = done

e3:
    sequence = 3
    recorded = 13:00
    effective = 11:00
    status = doing
```

Query:

```text
effective time = 13:00
known time = 14:00
```

All three events are visible.

### Transaction-order interpretation

Last recorded applicable event wins:

```text
status = doing
```

### Valid-time interpretation

The assertion with the latest valid start wins:

```text
status = done
```

### Historical-correction interpretation

The backdated `doing` assertion changes only the interval before the later `done` assertion:

```text
[10:00, 11:00) = open
[11:00, 12:00) = doing
[12:00, ∞)     = done
```

This is usually what users expect from bitemporal facts.

## 8.3 Recommended semantic model

For scalar part values, interpret each assertion as beginning at `effective_at`.

For a selected known time `K`:

1. include only events with `recorded_at ≤ K`;
2. apply supersession/retraction visible by `K`;
3. build a valid-time timeline ordered by `effective_at`;
4. use transaction order as a tie-breaker for events with the same effective time.

For a target `x`, valid time `V`, and known time `K`:

```text
Candidates(x,V,K) =
{
    e |
    target(e) = x
    ∧ recorded_at(e) ≤ K
    ∧ effective_at(e) ≤ V
    ∧ not superseded_as_of(e,K)
}
```

Recommended winner:

```text
Winner(x,V,K) =
arg max over Candidates:
    (
        effective_at,
        recorded_at,
        stream_sequence,
        event_id
    )
```

The event ID is only a deterministic final tie-breaker.

This produces historical-correction behavior:

```text
a backdated assertion applies from its effective time
until a later effective assertion takes precedence
```

## 8.4 Why stream sequence remains necessary

Stream sequence is still important for:

- deterministic transaction order;
- exact history rendering;
- same-effective-time conflicts;
- append preconditions;
- replication or export order;
- audit chronology.

But sequence should not silently replace valid-time order if the system calls the projection bitemporal.

## 8.5 Retraction semantics

A retraction needs an explicit meaning.

Recommended:

```text
retract-value at effective time T
    means:
    no current value from T
    until a later effective assertion
```

It does not mean:

```text
the value never existed
```

At a known time before the retraction was recorded, the retraction is invisible.

Example:

```text
e1 recorded 10:00, effective 09:00, value A
e2 recorded 14:00, effective 11:00, retract
```

Queries:

```text
V=12:00, K=13:00 → A
V=12:00, K=15:00 → no value
```

## 8.6 Supersession semantics

Current supersession handling should be strengthened with rules.

At minimum:

```text
Supersedes(new, old)
    →
old exists
∧ same stream
∧ new.recorded_at ≥ old.recorded_at
```

Decide whether supersession must target:

```text
same logical part
same operation family
same claim
```

A safe initial rule:

```text
scalar correction:
    same canonical target

link correction:
    same canonical link assertion

cross-target correction:
    requires a distinct explicit relation, not Supersedes
```

Supersession visibility must itself be bitemporal:

```text
old is hidden only in projections where the superseding event is known
```

## 8.7 Future-effective assertions

A future assertion is known now but not yet valid.

Example:

```text
recorded = Monday
effective = Friday
status = done
```

Queries:

```text
V=Wednesday, K=Thursday → old status
V=Saturday,  K=Thursday → done
```

This separation should be explicitly tested.

## 8.8 Proposed truth-table suite

For each operation family, test:

| Case | Recorded order | Effective order | Expected |
|---|---|---|---|
| Normal update | same | same | later value |
| Backdated update | later | earlier | interval correction |
| Future update | now | future | invisible before valid time |
| Same effective time | different | same | later recorded wins |
| Retraction | later | later | no value after effective time |
| Backdated retraction | later | earlier | historical interval removed in later-known view |
| Supersession | later | any allowed | old hidden only when superseder is known |
| Out-of-order import | arbitrary | arbitrary | deterministic timeline |
| Recursive move | later | earlier | hierarchy reconstructed by valid time |
| Link retraction | later | earlier | relation interval reconstructed |

## 8.9 Implementation approach

Do not rely on one global event sort for every semantic operation.

A more explicit projector can:

```text
1. filter by known time
2. group events by canonical target or relation identity
3. construct each target timeline
4. evaluate timeline at requested valid time
5. assemble hierarchy and links
6. validate global invariants
```

This is easier to reason about than:

```text
sort all events once
then rely on mutation order to imply bitemporal semantics
```

## 8.10 Acceptance criteria

- [ ] The specification defines `recorded_at`, `effective_at`, and stream sequence separately.
- [ ] Scalar winner behavior is stated mathematically.
- [ ] Retraction has interval semantics.
- [ ] Supersession visibility is time-scoped.
- [ ] Backdated updates have deterministic expected outputs.
- [ ] Same-time conflicts have deterministic tie-breakers.
- [ ] Tests cover all truth-table cases.
- [ ] A second cleanroom implementation can produce identical projections.

---

# 9. Finding 4 — Accepted operations can lack explicit semantics

## 9.1 Verdict

**Confirmed.**

The validator recognizes operations including:

```text
assign-ontology
remove-ontology
join-scope
leave-scope
observe-effect
attach-evidence
record-verification
```

The current projector visibly implements:

- part operations;
- link operations;
- subtree operations;
- restore and superseding value behavior.

Several recognized operations have no corresponding explicit application branch in the projector.

## 9.2 Why this is dangerous

A caller can potentially create an event satisfying:

```text
valid operation name
∧ required generic fields
∧ storable JSON
```

but:

```text
projection_after = projection_before
```

That is not always wrong. Some events are legitimate audit-only markers.

The problem is that the system does not distinguish:

```text
intentionally projection-neutral marker
```

from:

```text
operation accepted before implementation exists
```

Without that distinction:

- SDK and CLI behavior can diverge;
- plugins can emit events that disappear from every view;
- users can believe ontology/evidence state was applied when it was only logged;
- cleanroom implementations can choose different interpretations;
- future code may reinterpret old events retroactively.

## 9.3 Required invariant

Use:

```text
Accepted(op)
    ⇔
Registered(op)
    ∧ ValidateSemantics(op)
    ∧ (
        ApplyProjection(op)
        ∨ ExplicitlyProjectionNeutral(op)
      )
```

Never use:

```text
operation appears in a string allowlist
    ⇒ accepted
```

## 9.4 Operation registry

Suggested design:

```go
type OperationDescriptor struct {
    Name              Operation
    Version           uint32
    ProjectionNeutral bool
    Validate          func(Context, Projection, Event) error
    Apply             func(*Projection, Event) error
}

type OperationRegistry interface {
    Lookup(Operation) (OperationDescriptor, bool)
}
```

Validation flow:

```text
descriptor = registry.Lookup(event.Operation)

if absent:
    reject unsupported_operation

if descriptor.ProjectionNeutral:
    descriptor.Apply must be nil or a no-op by contract
else:
    descriptor.Apply must exist

descriptor.Validate(...)
descriptor.Apply(...)
```

## 9.5 Marker events

Examples that may be projection-neutral:

```text
create-entity
maintenance-started
integrity-check-completed
checkpoint-published
```

Even marker events should have a documented observable location:

```text
history
provenance
maintenance report
```

“Projection neutral” should mean:

```text
does not change ticket current-state projection
```

not:

```text
invisible everywhere
```

## 9.6 Versioning

Operation semantics become part of the durable format.

Use either:

```text
operation = "set-value"
operation_version = 1
```

or:

```text
operation = "set-value@1"
```

Changing the meaning of an existing operation without a version change can alter old projections.

## 9.7 Unknown-operation policy

For a canonical implementation:

```text
unknown operation in append:
    reject

unknown operation in existing store:
    fail projection with structured unsupported-operation error
```

A tolerant import tool may preserve an unknown event as opaque, but it must not claim a complete projection.

## 9.8 Tests

Generate one table-driven test per operation:

```text
operation
valid target kinds
required value kind
projection effect
allowed preconditions
inverse/compensating operation
```

For every registered operation:

1. create valid event;
2. confirm validation succeeds;
3. confirm expected projection or marker view changes;
4. create invalid target/value cases;
5. confirm validation fails;
6. round-trip serialization;
7. deterministic replay.

Add a registry completeness test:

```go
for _, op := range AllDeclaredOperations {
    if _, ok := registry.Lookup(op); !ok {
        t.Fatalf("declared operation without handler: %s", op)
    }
}
```

## 9.9 Acceptance criteria

- [ ] The string allowlist is replaced by an operation registry.
- [ ] Every accepted operation is explicitly state-changing or explicitly marker-only.
- [ ] Operation semantics are versioned.
- [ ] Unknown operations fail with a stable machine error.
- [ ] The registry has completeness tests.
- [ ] Runtime ontology/plugins cannot emit unregistered operations.

---

# 10. Finding 5 — Markdown duplicate-heading and ingestion-loss risks

## 10.1 Verdict

**Duplicate sibling handling appears buggy. Text before the first heading is also currently ignored.**

## 10.2 Duplicate-heading behavior

Desired input:

```markdown
## Evidence
A

## Evidence
B

## Evidence
C
```

Desired paths:

```text
evidence
evidence-2
evidence-3
```

The current parser keeps a `used` map, but after creating `evidence-2` it increments the rewritten key instead of the base sibling key.

The likely sequence is:

```text
first:
    used["evidence"] = 1

second:
    sees used["evidence"] = 1
    emits "evidence-2"
    increments used["evidence-2"]

third:
    still sees used["evidence"] = 1
    emits "evidence-2" again
```

## 10.3 Correct algorithm

Track occurrence count by the unsuffixed sibling base path.

```go
basePath := append(parentPath, segment)
baseKey := strings.Join(basePath, "/")

count := used[baseKey] + 1
used[baseKey] = count

actualSegment := segment
if count > 1 {
    actualSegment = fmt.Sprintf("%s-%d", segment, count)
}

path := append(parentPath, actualSegment)
```

Also check the final path against all existing paths in case user headings naturally include suffixes:

```text
Evidence
Evidence 2
Evidence
```

A collision allocator should loop until unique.

## 10.4 Preamble loss

For non-heading lines:

```go
if level == 0 {
    if len(stack) > 0 {
        append to current node
    }
    continue
}
```

When no heading has been encountered, the line is discarded.

Example:

```markdown
This introductory context is important.

## Problem

...
```

The introductory paragraph does not become a part.

This conflicts with a provenance-first ingestion principle:

```text
accepted input should not be silently discarded
```

## 10.5 Recommended Markdown data-loss contract

Use:

```text
Parse(input)
    →
parts + diagnostics
```

The parser must classify every byte into one of:

```text
semantic part content
structural syntax
explicitly ignored syntax with diagnostic
unsupported syntax preserved as opaque content
```

A useful invariant:

```text
ConcatenatePreservedSourceSpans(parts, metadata, ignored-spans)
    covers the complete input byte range
```

This does not mean exporting must reproduce every byte exactly, but ingestion must know what happened to each byte.

## 10.6 Root and title policy

Define the mapping clearly.

Recommended:

```text
first H1:
    ticket title candidate

body below H1 before H2:
    root/context or description part

text before first heading:
    root/preamble part

H2+:
    nested parts
```

Alternative:

```text
entire document is a source artifact
parsed parts link back to source spans
```

This is strongest for provenance because no parsing decision destroys the original document.

## 10.7 Stable identity across reimport

Heading-generated paths are not enough to preserve identity after reordering or renaming.

Support optional explicit IDs:

```markdown
## Race test {#race-test}
```

or front matter:

```yaml
missis:
  part_id: part:01...
```

Policy:

```text
explicit ID:
    authoritative identity hint

same source artifact + same explicit ID:
    update existing part

generated slug only:
    heuristic matching with diagnostic
```

## 10.8 Other parser test cases

Add tests for:

- three identical sibling headings;
- identical headings under different parents;
- a natural `evidence-2` plus repeated `evidence`;
- empty headings;
- Unicode-only headings;
- punctuation-only headings;
- skipped heading levels (`H2` to `H5`);
- fenced code containing `#`;
- ATX closing hashes (`## Title ##`);
- headings inside blockquotes;
- preamble before first heading;
- root body after H1;
- document with no headings;
- CRLF input;
- very large sections;
- repeated import with reordered sections;
- explicit stable IDs;
- maliciously deep/large input.

## 10.9 Parsing implementation choice

The handwritten parser is acceptable for a deliberately small Markdown subset, but the supported subset must be declared.

Two valid choices:

### Restricted Missis Markdown

```text
only ATX headings define structure
everything else is opaque body text
```

Simple and predictable.

### Full CommonMark AST

Use an AST parser and map actual heading nodes, avoiding false headings inside code blocks.

The current line parser can misinterpret certain Markdown constructs unless the subset is explicit.

## 10.10 Acceptance criteria

- [ ] Repeated siblings produce deterministic unique paths.
- [ ] Text before the first heading is preserved.
- [ ] Every input byte is preserved or covered by a diagnostic.
- [ ] Markdown subset or CommonMark behavior is documented.
- [ ] Explicit IDs can stabilize reimport identity.
- [ ] Round-trip tests cover repeated headings and preambles.
- [ ] Parsed parts retain source artifact and source span provenance.

---

# 11. Finding 6 — Lineage currently returns a traversal tree, not necessarily a complete graph

## 11.1 Verdict

**Confirmed by traversal logic.**

The walker marks a canonicalized key as visited and skips every later edge reaching that node.

This prevents infinite recursion, but it also removes converging edges.

## 11.2 Diamond example

```text
      A
     / \
    v   v
    B   C
     \ /
      v
      D
```

Edges:

```text
A supports B
A supports C
B derived-from D
C verified-by D
```

A node-visited traversal may include:

```text
A → B
B → D
A → C
```

and omit:

```text
C → D
```

The omitted edge is not redundant. It represents a second reason or provenance route.

## 11.3 Tree and graph are different views

### Navigation tree

Purpose:

```text
show a concise cycle-safe route outward
```

Properties:

- each node appears once;
- one parent route is selected;
- output is small;
- useful for terminal navigation.

### Complete reachable graph

Purpose:

```text
show every qualifying relation among reachable nodes
```

Properties:

- a node may have many incoming/outgoing edges;
- parallel relations remain;
- cycles are represented;
- useful for provenance, verification, visualization, and algorithms.

Both should exist.

## 11.4 Recommended graph algorithm

### Phase 1 — discover reachable nodes

Use BFS with:

```text
node expansion visited set
depth per node
relation filter
direction policy
```

### Phase 2 — collect edges

Collect every active edge satisfying:

```text
source in reachable set
∧ target in reachable set
∧ relation allowed
∧ direction allowed
∧ temporal predicate satisfied
```

Use an edge-identity set to prevent duplicate serialization, not a node set.

```text
EdgeKey =
    canonical from
    × relation
    × canonical to
    × assertion identity or projected relation identity
```

### Alternative one-pass approach

Record every edge before checking whether the target node has already been expanded:

```text
for edge from current:
    if edge not emitted:
        emit edge

    if target not expanded:
        expand target
```

## 11.5 Cycle behavior

Example:

```text
A caused-by B
B caused-by C
C caused-by A
```

Graph output should contain all three edges while traversal terminates.

Tree output may show only:

```text
A → B → C
```

and annotate:

```text
C → A (cycle/back-edge)
```

## 11.6 Depth semantics

Define whether edge depth means:

```text
depth of source
depth of target
length of shortest path from start
```

Recommended:

```text
node_depth(start) = 0
edge_depth(u → v) = node_depth(u) + 1
```

For graph mode, an edge may connect nodes at equal or lower depths. Include both endpoint depths in JSON if needed.

## 11.7 Temporal lineage

Lineage should accept:

```text
effective_at
known_at
```

The active relation set must be projected under the same temporal semantics as tickets.

Do not traverse current links when the user requested a historical ticket state.

## 11.8 Evidence independence

Multiple edges may mean independent support.

Example:

```text
test run 1 supports claim
test run 2 supports claim
reviewer supports claim
```

A complete graph must not collapse them merely because the target claim is the same.

## 11.9 Tests

- diamond graph;
- directed cycle;
- parallel edges with different relations;
- multiple assertions of same relation;
- inverse traversal;
- both-direction traversal;
- historical retraction;
- relation filter;
- depth 0/1/N;
- moved part aliases;
- graph mode versus tree mode output.

## 11.10 Acceptance criteria

- [ ] Tree and graph views are explicitly different.
- [ ] Graph view retains converging and cyclic edges.
- [ ] Traversal terminates on cycles.
- [ ] Edge identity is independent of mutable paths.
- [ ] Temporal parameters affect lineage consistently.
- [ ] JSON declares view type and depth semantics.

---

# 12. Finding 7 — CLI, SDK, TUI, and future APIs need one application layer

## 12.1 Verdict

**Confirmed architectural drift risk.**

The README states that the CLI should remain thin and `pkg/missis` should be the reusable facade.

The current command file is large and contains:

- flag parsing;
- global maintenance behavior;
- agent onboarding;
- context discovery;
- command orchestration;
- output types;
- rendering decisions;
- direct imports of both SDK and lower-level implementation packages.

The SDK exposes option structures whose behavior is not yet uniformly complete.

Examples observed in the reviewed source include:

- project information present in `NewTicketOptions` but not visibly applied by the high-level creation path;
- `SetPartOptions` containing operations broader than the narrower `SetPart` implementation path;
- search implemented as an in-memory substring scan;
- CLI behavior using lower-level packages in addition to the SDK.

## 12.2 Why drift is dangerous

Let:

```text
B_cli(x) = behavior through CLI
B_sdk(x) = behavior through Go SDK
B_tui(x) = behavior through TUI
```

Required:

```text
EquivalentRequest(x)
    →
EquivalentDomainResult(
    B_cli(x),
    B_sdk(x),
    B_tui(x)
)
```

Without one application layer, each interface can:

- validate differently;
- assign different defaults;
- use different actor identities;
- generate different event batches;
- return different errors;
- implement new features at different times;
- produce different provenance.

## 12.3 Recommended architecture

```text
                 ┌──────────────┐
CLI ────────────►│              │
TUI ────────────►│ Application  │──► Domain model
Go SDK ─────────►│ Service      │──► Ledger port
Future API ─────►│              │──► Projection port
Agent adapter ──►│              │──► Processor port
                 └──────────────┘
```

The public domain surface can still preserve the three verbs:

```go
type Service interface {
    New(ctx context.Context, req NewRequest) (NewResult, error)
    Show(ctx context.Context, query ShowQuery) (ShowResult, error)
    Set(ctx context.Context, mutation SetRequest) (SetResult, error)
}
```

Maintenance is separate:

```go
type MaintenanceService interface {
    Health(ctx context.Context, req HealthRequest) (HealthReport, error)
    Verify(ctx context.Context, req VerifyRequest) (IntegrityReport, error)
    Backup(ctx context.Context, req BackupRequest) (BackupReceipt, error)
    RepairPlan(ctx context.Context, req RepairRequest) (RepairPlan, error)
}
```

This preserves:

```text
three domain commands
```

without pretending backup or verification are issue mutations.

## 12.4 Request algebra

`SetRequest` should be a tagged union, not a bag of loosely interacting booleans.

Avoid:

```go
type SetPartOptions struct {
    Add       bool
    Retract   bool
    Recursive bool
    Name      string
    Parent    string
}
```

Prefer:

```go
type Mutation interface {
    isMutation()
}

type SetValue struct {
    Target Ref
    Value  Value
}

type AddValue struct {
    Target Ref
    Value  Value
}

type RetractValue struct {
    Target Ref
    Reason string
}

type RetractSubtree struct {
    Target Ref
    Reason string
}

type RenamePart struct {
    Target Ref
    Name   string
}

type MovePart struct {
    Target Ref
    Parent *Ref
}
```

The CLI parser converts flags into exactly one mutation.

This prevents invalid combinations such as:

```text
--retract + --add + --name
```

from leaking deep into the system.

## 12.5 Error contract

Use domain error codes independent of CLI exit codes:

```text
invalid_input
not_found
validation_failed
conflict
unsupported_operation
integrity_failed
permission_denied
processor_failed
storage_failed
```

The CLI maps:

```text
domain error → exit code
```

The SDK returns typed errors.

The TUI renders the same errors.

## 12.6 Time and actor injection

The service should receive:

```go
type RequestContext struct {
    Actor          ActorRef
    EffectiveAt    time.Time
    KnownAt        time.Time
    IdempotencyKey string
}
```

`recorded_at` must remain system-controlled.

Inject a clock for tests:

```go
type Clock interface {
    Now() time.Time
}
```

This makes temporal behavior deterministic.

## 12.7 Package layout

A stable direction:

```text
cmd/missis/
    parsing and rendering only

pkg/missis/
    public client/API types

internal/application/
    New/Show/Set orchestration

internal/domain/
    events, refs, parts, links, ontology contracts

internal/store/sqlite/
    persistence implementation

internal/projection/
    temporal projectors

internal/integrity/
    hash/checkpoint verification

internal/search/
    rebuildable search projections
```

Move implementation packages under `internal/` before third parties depend on them.

## 12.8 Refactoring sequence

Do not rewrite the CLI.

### Step 1

Create an application service that wraps the existing behavior.

### Step 2

Move one workflow at a time:

```text
show ticket
new ticket
set scalar
links
Markdown
projects/groups
maintenance
```

### Step 3

Keep black-box tests unchanged.

### Step 4

Add SDK equivalence tests:

```text
same request through CLI and SDK
    →
same events and same projection
```

### Step 5

Remove lower-level imports from the CLI.

## 12.9 Acceptance criteria

- [ ] CLI domain workflows call one application service.
- [ ] SDK uses the same service.
- [ ] TUI does not recreate domain mutations.
- [ ] `Set` is represented as a validated mutation union.
- [ ] Defaults for actor/time/idempotency exist in one place.
- [ ] Domain errors are stable and interface-independent.
- [ ] CLI imports no SQLite store implementation directly.
- [ ] Black-box behavior remains compatible.

---

# 13. Finding 8 — Foundational performance will become a constraint before advanced search

## 13.1 Verdict

**Confirmed by current data paths, though acceptable for early alpha.**

Several current operations scale with total history.

## 13.2 Current cost centers

### Normal open

As described earlier:

```text
O(total events)
```

because hashes are rebuilt.

### Append

The append path loads existing events through `loadEventsTx`.

It then validates each proposed event by adding it to the running event slice and projecting.

For batch size `B` and total store event count `N`, the rough upper-level behavior can approach:

```text
O(B × N)
```

plus sorting and path rebuilding.

Even though `ProjectTicket` filters one stream, it receives the larger event collection in validation.

### List tickets

`ListTickets`:

1. loads all events;
2. groups them by ticket;
3. projects each ticket;
4. looks up aliases.

This is reasonable at small scale but eventually makes:

```bash
missis show
```

a full-ledger computation.

### Search

Current substring search projects tickets and concatenates string values.

This compounds the list/projection cost.

## 13.3 Authoritative versus rebuildable state

Keep:

```text
events = source of truth
```

Add rebuildable acceleration:

```text
stream_heads
ticket_current
part_current
part_paths_current
link_current
scope_membership_current
projection_checkpoints
search_documents
ontology_obligations_current
```

The invariant:

```text
RebuildDerivedState(events) = StoredDerivedState
```

Derived rows can be deleted and rebuilt.

## 13.4 Stream-specific append validation

Append validation should load:

```text
events for target stream
relevant cross-stream references
current stream head
precondition targets
```

not every event in the store.

For a ticket mutation:

```text
N_ticket ≪ N_store
```

Desired append cost:

```text
O(N_ticket + batch)
```

before snapshots, then:

```text
O(tail_since_snapshot + batch)
```

after snapshots.

## 13.5 Projection checkpoints

A checkpoint can contain:

```text
stream ID
last sequence
last event ID
projection format version
projector version
projection bytes/hash
created time
```

Current state:

```text
Projection_at_head =
    Apply(
        checkpoint_projection,
        events_after_checkpoint
    )
```

A checkpoint is disposable.

Never let a checkpoint become the only copy of state.

## 13.6 Incremental current projections

Within the same append transaction:

```text
append events
    +
update current projection rows
    +
advance stream head
    +
extend integrity head
```

All succeed or fail atomically.

A verification tool periodically rebuilds projections from events and compares.

## 13.7 Path index

Current path lookup should be indexed by:

```text
ticket_id
effective interval or current flag
path
part_id
```

For the current projection:

```sql
UNIQUE(ticket_id, path)
```

Historical path aliases belong in a separate temporal table or are reconstructed from events.

## 13.8 Link adjacency

Maintain rebuildable indexes:

```text
links_by_from(canonical_ref)
links_by_to(canonical_ref)
```

Do not query all link events for every lineage operation after scale increases.

## 13.9 Benchmark plan

Create synthetic ledgers:

```text
tickets: 10, 1k, 100k
events: 1k, 100k, 1M
average parts/ticket: 10, 100
average links/ticket: 0, 5, 50
history depth: 1, 20, 1k
```

Measure:

- cold open;
- warm open;
- current ticket show;
- historical ticket show;
- list current tickets;
- append scalar;
- append recursive Markdown batch;
- lineage depth 1/3/10;
- search;
- full integrity verification;
- projection rebuild.

Set performance budgets before optimizing.

Example initial local budgets:

```text
normal open:
    p95 < 50 ms at 100k events

show one current ticket:
    p95 < 25 ms after open

append one scalar:
    p95 < 50 ms without contention

list 1k tickets:
    p95 < 150 ms

full verify:
    allowed O(N), separately reported
```

These are proposed budgets, not confirmed current measurements.

## 13.10 Acceptance criteria

- [ ] Normal open is independent of total event count except migration/explicit full verify.
- [ ] Append does not load unrelated streams.
- [ ] Current list does not fold the entire history every time.
- [ ] Derived tables are explicitly rebuildable.
- [ ] Projection rebuild equivalence is tested.
- [ ] Benchmarks cover growth and concurrency.
- [ ] Performance work precedes expensive vector-search infrastructure.

---

# 14. Finding 9 — Search should use parts as canonical hits

## 14.1 Verdict

**Current search is a valid bootstrap, but the public abstraction should not stabilize around ticket-only results.**

The natural searchable unit in Missis is:

```text
Part
```

not the complete ticket blob.

## 14.2 Why ticket-only hits are limiting

A ticket can contain:

```text
problem
hypothesis
evidence/race-test/run-417/stderr
code/retry-loop
verification/stress-test
decision
```

A query may match one precise claim.

Returning only:

```text
#184
```

loses:

- which part matched;
- what text matched;
- provenance of that part;
- temporal visibility;
- relation to evidence;
- exact score contribution;
- reusable canonical identity.

## 14.3 Canonical search result

```go
type PartHit struct {
    PartID       PartID
    TicketID     TicketID
    CurrentPath  string
    Breadcrumb   []string

    Value        Value
    ValueKind    ValueKind
    Types        []OntologyRef

    ProjectIDs   []ProjectID
    GroupIDs     []GroupID

    RecordedAt   time.Time
    EffectiveAt  time.Time
    CurrentFrom  EventID

    Sources      []SourceRef
    Links        []LinkSummary

    Match        MatchExplanation
    Scores       ScoreBreakdown
}
```

Where:

```go
type ScoreBreakdown struct {
    BM25       *float64
    Vector     *float64
    Graph      *float64
    Metadata   *float64
    Recency    *float64
    Reranker   *float64
    Final      float64
}
```

## 14.4 Search pipeline

```text
query
  ↓
query parsing
  ↓
candidate generators
  ├── BM25 / full-text
  ├── vector similarity
  ├── exact reference lookup
  ├── metadata filter
  ├── temporal filter
  ├── code-symbol lookup
  └── graph/lineage expansion
  ↓
candidate union
  ↓
deduplicate by canonical PartID
  ↓
score normalization or rank fusion
  ↓
reranker
  ↓
part hits
  ↓
optional ticket aggregation
```

## 14.5 Rank fusion

BM25 and vector scores are not directly comparable.

Use a rank-based method initially, such as reciprocal rank fusion:

```text
RRF(d) =
Σ over retrievers r:
    1 / (k + rank_r(d))
```

This avoids pretending heterogeneous raw scores share a scale.

Later, a learned reranker can consume:

- query;
- part text;
- parent context;
- ticket title;
- ontology type;
- relation context;
- provenance;
- recency;
- lexical/vector features.

## 14.6 Temporal search

Search must first select a temporal projection.

```text
Search(q, effective_at, known_at)
```

A part retracted before the selected valid time should not appear in a current-state search unless historical search is requested.

Useful modes:

```text
current:
    current visible parts

history:
    exact historical event/part versions

changed-since:
    events or parts changed after time

known-at:
    what the system knew at a past transaction time
```

## 14.7 Embedding provenance

Embeddings are derived artifacts.

Store:

```text
source PartID
source event/version
embedding model ID
model version/hash
normalization method
chunking method
generated_at
processor invocation ID
```

Do not store vectors as authoritative ticket facts.

Invariant:

```text
delete all embeddings
    →
source ledger remains complete
```

## 14.8 Parent and subtree context

Search each part independently, but allow retrieval context:

```text
part text
+
ancestor headings
+
ticket title
+
selected linked claims
```

Do not concatenate the entire ticket into every part document because this causes duplication and weakens precision.

## 14.9 Search explanations

Agent-friendly output should explain:

```text
why this matched
which part matched
which retrievers returned it
which filters applied
which lineage expansion added it
which reranker selected it
```

Example:

```json
{
  "ref": "#184/evidence/race-test/run-417/error",
  "part_id": "part:01...",
  "matched": ["retry", "cancellation"],
  "candidate_sources": ["bm25", "vector"],
  "expanded_from": "#184/hypothesis",
  "final_rank": 1
}
```

## 14.10 Acceptance criteria

- [ ] Search core returns canonical `PartHit` values.
- [ ] Ticket-level results are an aggregation view.
- [ ] Search respects effective and known time.
- [ ] BM25, vector, metadata, and graph candidates deduplicate by `PartID`.
- [ ] Derived indexes can be rebuilt.
- [ ] Embeddings retain model and source provenance.
- [ ] JSON includes an explanation suitable for agents.

---

# 15. Finding 10 — Store discovery is a security boundary

## 15.1 Verdict

**Confirmed design concern.**

Current discovery order is documented as:

```text
--store
nearest .missis marker
MISSIS_STORE
XDG fallback
```

A marker can contain an absolute path or a relative path.

A repository therefore influences the database used by commands run inside it.

## 15.2 Threat scenarios

### Malicious cloned repository

Repository contains:

```text
.missis:
    ../../some-sensitive-or-shared-path.db
```

A user runs:

```bash
missis new "test"
```

Missis writes outside the repository.

### Absolute path marker

A repository points to:

```text
/home/user/shared/missis.db
```

The user may believe work is isolated locally.

### Symlink substitution

A path component or target is replaced with a symlink between validation and open.

### Environment shadowing

A trusted automation supplies `MISSIS_STORE`, but the repository marker has higher precedence and silently redirects it.

### Accidental worktree sharing

Two worktrees write to one SQLite store even though branch histories and code contexts differ.

## 15.3 Trust policy

Distinguish sources by authority.

Recommended precedence:

```text
1. explicit --store
2. explicit MISSIS_STORE
3. trusted project marker
4. user XDG default
```

Reason:

```text
process/user explicit input
    should outrank
repository-controlled input
```

Another acceptable policy is marker-before-env, but it must be a deliberate documented decision and automation must have an explicit bypass.

## 15.4 Marker constraints

Default safe marker policy:

```text
relative path only
resolved path must remain under repository root
default target: .missis-store/missis.db
absolute path requires --allow-external-store
parent traversal outside root rejected
```

For a shared store, require explicit configuration:

```bash
missis --store /shared/path/missis.db ...
```

or a trusted marker declaration containing a policy version.

## 15.5 Symlink policy

At minimum:

- resolve absolute normalized path;
- inspect symlinks;
- display the final resolved path;
- optionally reject symlink targets by default;
- verify ownership/permissions where supported;
- open with platform-appropriate secure flags;
- avoid following a changed path after validation where possible.

Perfect TOCTOU protection is platform-specific, but the policy must be explicit.

## 15.6 Permissions

Current directory creation uses `0755`.

Safer defaults for personal provenance data:

```text
store parent directory:
    0700

database:
    0600

backup:
    0600
```

Provide an explicit shared mode:

```bash
missis --init --shared-group <group>
```

Do not accidentally infer sharing from existing permissive directories.

## 15.7 Observability

Every health/context report should show:

```text
store path as supplied
resolved absolute path
discovery source
store ID
schema version
head hash/checkpoint
read-only/read-write mode
permission warning
worktree/repository root
```

This helps humans and agents detect redirection.

## 15.8 Tests

- marker relative inside repository;
- marker with `../` escaping root;
- absolute marker;
- symlink marker;
- symlink parent;
- explicit `--store` precedence;
- `MISSIS_STORE` precedence;
- nested repository marker discovery;
- Windows drive and UNC paths;
- inaccessible file;
- overly permissive file warning;
- two worktrees using the same store warning.

## 15.9 Acceptance criteria

- [ ] Discovery order has one canonical specification.
- [ ] Repository-controlled paths cannot escape the repository by default.
- [ ] External/absolute stores require explicit trust.
- [ ] Store and backup permissions default to private.
- [ ] Health output shows resolved path and discovery source.
- [ ] Symlink behavior is documented and tested.
- [ ] Worktree store sharing is detected or explicitly opted into.

---

# 16. Finding 11 — Cleanroom portability requires canonical event encoding

## 16.1 Verdict

**Confirmed future compatibility risk.**

The event hash currently depends on Go's JSON serialization of the event structure.

Even if Go serialization is deterministic for current values, the durable cleanroom contract must not depend on undocumented language-runtime behavior.

## 16.2 Sources of divergence

Different implementations may encode the same semantic event differently:

- object key order;
- absent field versus `null`;
- integer versus floating-point number;
- exponent formatting;
- timestamp precision;
- timezone offset versus UTC `Z`;
- Unicode normalization;
- escaped versus unescaped characters;
- map ordering inside `Value.Data`;
- unknown fields;
- default zero values;
- byte arrays;
- future value kinds.

Therefore:

```text
SemanticEqual(event_A, event_B)
```

does not imply:

```text
JSONBytes_A = JSONBytes_B
```

## 16.3 Canonical encoding contract

Define:

```text
CanonicalEventBytes(event, format_version) → byte string
```

This must be specified independently of Go.

Two practical options:

### Canonical JSON

Use a documented canonical JSON scheme, such as a restricted schema plus JSON Canonicalization Scheme principles.

Requirements:

- deterministic key order;
- UTF-8;
- exact timestamp format;
- exact number rules;
- no NaN/Infinity;
- no duplicate object keys;
- defined Unicode policy;
- defined absent/null behavior.

### Deterministic CBOR

Use deterministic CBOR with a fixed schema.

This is compact but less human-readable and introduces another serialization layer.

Canonical JSON is probably more aligned with Missis's cleanroom and inspection goals.

## 16.4 Domain separation

Avoid hashing ambiguous text concatenation such as:

```text
previousHash + "\n" + JSON
```

Use domain-separated binary framing:

```text
HashInput =
    "MISSIS-EVENT-HASH" ||
    0x00 ||
    format_version ||
    length(previous_hash_bytes) ||
    previous_hash_bytes ||
    length(canonical_event_bytes) ||
    canonical_event_bytes
```

Then:

```text
hash = SHA-256(HashInput)
```

This prevents cross-protocol and framing ambiguity.

## 16.5 What belongs in canonical bytes

Include immutable event facts:

- event ID;
- stream identity;
- stream sequence;
- batch ID;
- operation and version;
- canonical target;
- value;
- recorded/effective time;
- actor;
- sources;
- inputs;
- causes;
- effects;
- supersedes;
- reason;
- ontology versions;
- invocation reference.

Exclude derived/local aliases if they are not portable:

```text
local numeric ticket alias
local event display alias
cached previous hash field
cached current hash field
```

The specification must explicitly decide each field.

## 16.6 Unknown fields

Two safe models:

### Closed schema per version

Unknown fields make the event invalid for that version.

Good for strict cleanroom reproducibility.

### Extension map included canonically

```text
extensions: map<string, canonical value>
```

Unknown extensions are preserved and hashed.

Do not silently discard unknown fields during decode/re-encode.

## 16.7 Test vectors

Publish test fixtures:

```text
event-v1-minimal.json
event-v1-full.json
event-v1-unicode.json
event-v1-numeric-boundaries.json
event-v1-extension.json
```

For each:

```text
canonical bytes hex
event hash with empty previous hash
event hash with specified previous hash
```

Implementations in Go, Rust, Python, or JavaScript should produce identical results.

## 16.8 Format evolution

Store:

```text
event_encoding_version
hash_algorithm
hash_format_version
```

Do not reinterpret old bytes under a new canonicalization rule.

A new format creates a new chain segment or explicitly migrates into a new store with a receipt.

## 16.9 Acceptance criteria

- [ ] Canonical encoding is specified outside Go implementation details.
- [ ] Hash input uses explicit framing and domain separation.
- [ ] Unknown-field behavior is defined.
- [ ] Timestamp and numeric encoding are exact.
- [ ] Cross-language test vectors are published.
- [ ] Encoding/hash versions are stored.
- [ ] Existing chains are not silently reinterpreted.

---

# 17. Finding 12 — Decide local-authoritative versus independently mergeable stores

## 17.1 Verdict

**Strategic decision required before sync/fork reconciliation is implemented.**

Current storage uses:

- local stream sequences;
- local numeric ticket aliases;
- a store identity;
- a single store-wide linear hash head;
- SQLite transactions;
- local backups and manifests.

This is naturally suited to a local-authoritative store.

It does not automatically support independently writable replicas.

## 17.2 Define terminology

### Backup

```text
copy of one authoritative store
used for restore
not independently writable
```

### Replica

```text
follows authoritative updates
may serve reads
does not independently create conflicting history
```

### Sync

Ambiguous. Avoid using it without defining direction and authority.

### Merge

```text
two independently advanced histories are reconciled
```

### Fork

```text
two stores share an ancestor but have different accepted descendants
```

## 17.3 Recommended Phase 1 contract

```text
Missis is local-authoritative.

One store is authoritative for one workspace.
Multiple processes may access it through SQLite concurrency.
Backups are not independently writable replicas.
Restore replaces or recovers the authoritative store.
Peer-to-peer merge is not supported.
```

This is a strong and useful product.

## 17.4 Why current structures conflict with offline merge

Suppose stores A and B fork after sequence 10.

Both append:

```text
A: stream sequence 11
B: stream sequence 11
```

Both can also allocate:

```text
ticket alias #42
```

Each extends a different global head.

A merge must decide:

- event order;
- stream sequence reassignment;
- alias collision;
- head-chain structure;
- conflicting scalar values;
- conflicting hierarchy moves;
- retractions of events absent from one side;
- ontology version availability.

Renumbering or rewriting old events is incompatible with immutable exact references.

## 17.5 If mergeability is added later

Introduce new primitives rather than stretching local sequence.

Possible model:

```text
EventID:
    globally unique

Origin:
    store/replica ID

OriginSequence:
    immutable monotonic sequence within origin

CausalParents:
    event IDs or frontier

RecordedTime:
    local transaction time

LogicalTime:
    HLC or explicit causal ordering

LocalDisplayAlias:
    non-portable convenience only
```

The merged ledger becomes a DAG rather than one linear chain.

Integrity becomes:

```text
per-origin chains
+
merge/checkpoint manifest
```

Conflict resolution must be operation-specific, not generic last-writer-wins.

## 17.6 Fork detection now

Even without merge, detect accidental fork/rollback:

```text
store_id
head_hash
event_count
last_event_id
checkpoint sequence
```

When restoring:

```text
candidate store head
versus
last trusted checkpoint
```

If the candidate is older or divergent, require an explicit restore/fork decision.

## 17.7 Acceptance criteria

- [ ] Phase 1 explicitly declares local-authoritative semantics.
- [ ] Backup, replica, sync, merge, and fork are defined separately.
- [ ] Restore detects rollback/divergence where checkpoint data exists.
- [ ] Numeric aliases are documented as store-local.
- [ ] No feature implies offline mergeability accidentally.
- [ ] A future merge design does not require rewriting accepted event IDs/bytes.

---

# 18. Finding 13 — Repository safeguards should match the provenance claim

## 18.1 Verdict

**The code-level test effort is strong, but repository enforcement is currently weak.**

At the reviewed commit:

- `main` is unprotected;
- no required status checks are configured;
- the reviewed commit is unsigned;
- no repository tags are published;
- the README demonstrates a `v0.1.0` install even though no such tag currently exists.

The reviewed commit does improve requirements traceability with a machine-readable registry and a two-way coverage gate. That is a positive step.

## 18.2 Required CI gates

Minimum pull-request pipeline:

```text
go test ./...
go test -race ./...
go vet ./...
build missis binary
run portable black-box suite
run requirement registry two-way check
run migration/reopen tests
run integrity corruption tests
run a bounded fuzz smoke suite
```

Recommended additional gates:

```text
static analysis
dependency vulnerability scan
license policy check
cross-platform build
format check
generated-file consistency check
```

## 18.3 Test tiers

### Tier 1 — fast per commit

Target under a few minutes:

- unit;
- black-box core;
- registry traceability;
- vet;
- build.

### Tier 2 — pull request

- race detector;
- corruption tests;
- migration matrix;
- moderate fuzz time;
- OS matrix.

### Tier 3 — scheduled/nightly

- long concurrency stress;
- large-store benchmarks;
- extended fuzzing;
- backup/restore with object storage;
- fault injection;
- cross-version compatibility.

## 18.4 Branch policy

For `main`:

- require pull request;
- require tests;
- prevent force push;
- prevent branch deletion;
- require conversation resolution;
- require up-to-date branch or merge queue;
- optionally require signed commits;
- require signed release tags.

For a one-person project, this still adds value because it protects against accidental bypass.

## 18.5 Release policy

Do one of:

### Option A

Remove non-existent `v0.1.0` example until a tag exists.

### Option B

Publish:

```text
v0.1.0-alpha.1
```

with:

- changelog;
- supported Go version;
- storage format version;
- compatibility warning;
- checksums;
- binaries for supported platforms;
- known limitations;
- no production-readiness claim.

## 18.6 Storage compatibility declaration

Every release should state:

```text
can read stores from versions:
can write format version:
downgrade supported:
backup required before migration:
migration reversible:
```

Never let self-update silently make a store unreadable by the previous binary without warning.

## 18.7 Acceptance criteria

- [ ] `main` has required checks.
- [ ] Traceability registry is enforced in CI.
- [ ] Race and black-box suites are required.
- [ ] Release examples refer to real tags.
- [ ] Alpha release documents store compatibility.
- [ ] Release artifacts have checksums and signed tags.
- [ ] Long stress/fuzz tests run on a schedule.

---

# 19. Finding 14 — Product positioning should say “simple interface,” not “simple system”

## 19.1 Verdict

**The product is differentiated, but “simplest tracker for any scenario” is broader than the current and intended system.**

Missis is simple at the verb level:

```text
new
show
set
```

It is deliberately sophisticated at the semantic level:

```text
events
bitemporality
recursive parts
canonical references
lineage
projects/groups
ontology
verification
processors
hybrid search
```

Both can be true.

## 19.2 Recommended positioning

Human-facing:

> Missis is a local-first issue tracker for humans and agents. It keeps the command surface small while preserving history, provenance, and precise references to every part of the work.

Technical:

> Missis is a temporal provenance and workflow kernel exposed through a three-command issue-tracker interface.

Agent-oriented:

> Missis gives agents a small deterministic protocol for creating, reading, updating, linking, and proving work without hiding history in one mutable issue body.

## 19.3 Progressive disclosure

### Level 0 — ordinary work

```text
new
show
set
```

### Level 1 — structured information

```text
parts
nested parts
Markdown import/export
```

### Level 2 — coordination

```text
status
links
projects
groups
```

### Level 3 — auditability

```text
history
recorded/effective time
sources
causes
effects
supersession
```

### Level 4 — correctness

```text
ontology
obligations
verification methods
processors
```

### Level 5 — retrieval and ecosystem

```text
BM25
vector search
reranking
code/Git references
external integrations
```

A user should not need Level 4 knowledge to create a normal ticket.

## 19.4 Define non-goals

Suggested current non-goals:

- not a Jira clone;
- not a general collaborative document editor;
- not peer-to-peer distributed in Phase 1;
- not a replacement for Git;
- not a secret manager;
- not a workflow engine with hundreds of status types;
- not production-ready while storage semantics remain alpha.

Non-goals increase credibility.

## 19.5 README structure

Recommended order:

1. one-sentence identity;
2. 60-second example;
3. why it differs;
4. alpha warning;
5. three-command model;
6. parts and links;
7. provenance/time;
8. install;
9. current limitations;
10. architecture/spec links;
11. contributor/test instructions.

Avoid leading with implementation vocabulary before showing the user outcome.

## 19.6 Acceptance criteria

- [ ] Messaging distinguishes interface simplicity from semantic power.
- [ ] README shows a minimal workflow first.
- [ ] Current limitations and non-goals are explicit.
- [ ] Agent and human benefits are both demonstrated.
- [ ] Advanced features are progressively disclosed.
- [ ] Claims do not imply universal scenario coverage or production stability.

---

# 20. Cross-cutting design rules

These rules should become specification-level invariants.

## 20.1 Identity

```text
Canonical identity is immutable.

Human-readable paths and aliases are temporal projections.

No mutable path participates in canonical equality.
```

## 20.2 Event immutability

```text
Once accepted:
    event ID, canonical bytes, stream, and sequence never change.
```

## 20.3 Projection reproducibility

```text
Same:
    canonical events
    projector version
    ontology versions
    query times

must produce:
    same projection
```

## 20.4 No hidden mutation

```text
CLI, SDK, processors, ontology hooks, repair tools, and search indexers
must not mutate authoritative state except by accepted events
or explicitly versioned store-maintenance transactions.
```

Store-maintenance transactions must never rewrite accepted event meaning.

## 20.5 Derived state

```text
search indexes
embeddings
snapshots
current projections
path indexes
link adjacency
obligation caches

are rebuildable
```

## 20.6 Temporal clarity

```text
recorded_at:
    when Missis accepted the event

effective_at:
    when the assertion applies in valid time

sequence:
    deterministic order of acceptance within a stream
```

None should silently substitute for another.

## 20.7 Effects versus claims

```text
intent ≠ action
action ≠ observed effect
observed effect ≠ verification
```

Each may reference the previous one.

## 20.8 Open-world parts, closed-world safety

```text
unknown part names:
    preserve

unknown operations:
    reject or preserve as explicitly opaque

unknown ontology behavior:
    do not execute
```

## 20.9 Repair transparency

```text
repair must increase knowledge about damage
not erase evidence that damage occurred
```

---

# 21. Recommended implementation roadmap

## Milestone 0 — Semantic freeze

Goal:

```text
make the core rules unambiguous before changing storage
```

Work:

1. define threat model;
2. define event immutability;
3. define sequence invariant;
4. define bitemporal winner;
5. define retraction and supersession;
6. define canonical reference key;
7. define operation registry behavior;
8. declare local-authoritative Phase 1 scope;
9. define canonical event encoding version 1.

Exit gate:

```text
spec truth tables
+
test vectors
+
no unresolved P0 semantic choice
```

## Milestone 1 — Ledger hardening

Dependencies:

```text
Milestone 0
```

Work:

1. stop hash rebuild on open;
2. implement verify/extend/repair separation;
3. remove in-place event renumbering;
4. add corruption and rollback tests;
5. add explicit repair receipts;
6. secure store discovery and permissions;
7. add branch CI protections;
8. publish an alpha storage compatibility statement.

Exit gate:

```text
accepted event bytes never change
tampering is detected
normal open is non-mutating
```

## Milestone 2 — Identity and projection correctness

Work:

1. canonicalize link graph keys;
2. add rename/move link tests;
3. implement formal bitemporal projector;
4. add operation registry;
5. distinguish graph versus tree lineage;
6. fix Markdown collisions and preamble preservation.

Exit gate:

```text
all identity/temporal truth-table tests pass
```

## Milestone 3 — One application service

Work:

1. create `internal/application`;
2. route CLI through it;
3. route SDK through it;
4. route TUI through it;
5. introduce mutation tagged unions;
6. stabilize typed errors;
7. move lower-level packages to `internal`.

Exit gate:

```text
CLI and SDK equivalent requests generate equivalent events
```

## Milestone 4 — Incremental projections and search foundation

Work:

1. stream-specific append validation;
2. current projection tables;
3. path and link indexes;
4. projection snapshots;
5. canonical `PartHit`;
6. BM25/full-text;
7. temporal filters;
8. graph expansion;
9. vector plugin later;
10. reranking later.

Exit gate:

```text
normal read/append performance meets declared budgets
search indexes rebuild from ledger
```

## Milestone 5 — Code/Git provenance

Work:

1. canonical repository identity;
2. immutable commit resolution;
3. path/range/symbol references;
4. Git diff and commit effects;
5. code-reference resolver processor;
6. reference drift diagnostics;
7. SafeDesign graph node integration.

Exit gate:

```text
ticket claim → code reference → change → commit → test evidence
is navigable as provenance
```

## Milestone 6 — Ontology and processors

Only after previous gates.

Processor contract:

```text
Processor(snapshot, inputs, capabilities)
    →
proposed events
+ evidence
+ diagnostics
+ declared observed effects
```

Requirements:

- deterministic input manifest;
- version and code hash;
- capability sandbox;
- idempotency;
- cycle/budget controls;
- no unrestricted DB mutation;
- all output appended through the application service.

Exit gate:

```text
processor behavior is provenance-bearing and replay-auditable
```

---

# 22. Suggested Missis tickets

The following can be created as independent work tickets.

## P0 tickets

### Correct integrity lifecycle

**Problem**

Normal open rebuilds and replaces the hash chain.

**Done when**

- open verifies rather than rebuilds;
- append extends;
- repair is explicit;
- corruption tests pass.

### Remove in-place event repair

**Problem**

Sequence repair updates accepted event bytes and sequences.

**Done when**

- accepted events are never updated;
- gaps cause incident/restore/new-store workflow;
- repair receipt exists.

### Specify bitemporal scalar semantics

**Problem**

Sequence-order folding and valid-time ordering can disagree.

**Done when**

- mathematical winner rule exists;
- truth-table tests pass;
- retraction/supersession semantics are defined.

## P1 tickets

### Canonicalize link identity

**Done when**

- graph keys omit path;
- move/rename tests pass;
- old-path reuse does not inherit links.

### Add operation registry

**Done when**

- every declared operation has validation and projection/marker semantics;
- unknown operations fail deterministically.

### Specify canonical event encoding v1

**Done when**

- language-independent canonical bytes are documented;
- test vectors pass in at least two implementations or independent encoders.

### Introduce application service

**Done when**

- CLI and SDK use the same New/Show/Set service;
- lower-level imports leave CLI.

### Harden store discovery

**Done when**

- repository markers cannot escape root by default;
- discovery source and resolved path are visible;
- private permissions are default.

### Declare local-authoritative storage contract

**Done when**

- backup/restore/fork/sync terminology is explicit;
- peer merge is a documented non-goal for Phase 1.

## P2 tickets

### Fix Markdown duplicate headings and preamble loss

### Add full lineage graph view

### Add projection snapshots and current indexes

### Make search return part hits

### Add protected CI and alpha release

### Rewrite positioning around local-first temporal provenance

---

# 23. Verification matrix

| Area | Unit | Model/property | Black-box | Fault/concurrency | Benchmark |
|---|---:|---:|---:|---:|---:|
| Hash chain | ✓ | ✓ | ✓ | ✓ | ✓ |
| Event immutability | ✓ | ✓ | ✓ | ✓ | — |
| Bitemporal projection | ✓ | ✓ | ✓ | — | ✓ |
| Canonical references | ✓ | ✓ | ✓ | — | — |
| Links/lineage | ✓ | ✓ | ✓ | — | ✓ |
| Markdown | ✓ | fuzz | ✓ | — | ✓ |
| Application service | ✓ | — | equivalence | — | ✓ |
| Store discovery | ✓ | property | ✓ | race/symlink where possible | — |
| Projection cache | ✓ | rebuild equivalence | ✓ | crash | ✓ |
| Search | ✓ | rank invariants | ✓ | rebuild | ✓ |
| Ontology/processors | ✓ | termination/budget | ✓ | sandbox/fault | ✓ |
| Backup/restore | ✓ | manifest invariants | ✓ | interrupted upload/restore | ✓ |

---

# 24. Definition of done for the provenance kernel

The kernel should not be called stable until all statements below are true.

```text
1. Accepted event bytes are immutable.

2. Normal open does not mutate authoritative or integrity state.

3. Corruption is detected before normal operations continue.

4. Event hashing is canonical across implementations.

5. Canonical references ignore mutable aliases.

6. Every operation has explicit semantics.

7. Bitemporal state has a formal winner rule.

8. Retraction and supersession are time-aware and auditable.

9. Current projections are reproducible from events.

10. Derived caches can be deleted and rebuilt.

11. CLI, SDK, and TUI use one application service.

12. Store path selection is visible and trust-aware.

13. Local-authoritative versus mergeable behavior is explicit.

14. Required tests are enforced on the protected main branch.
```

Formally:

```text
StableKernel
    ⇔
IdentityStable
∧ EventImmutable
∧ ProjectionDeterministic
∧ TemporalSemanticsDefined
∧ IntegrityVerifiable
∧ OperationsTotal
∧ InterfacesEquivalent
∧ RecoveryAuditable
```

---

# 25. Final judgment

Missis should continue.

The architecture has a real advantage over conventional issue trackers:

```text
Part
    gives precise addressability

Link
    gives virtual composition

Event
    gives time and provenance

Projection
    gives human- and agent-friendly views

Ontology
    can later give meaning and correctness obligations

Processor
    can later give extensibility without hidden mutation

New/Show/Set
    keeps the public protocol small
```

The project does not need a rewrite.

It needs a deliberate semantic-hardening phase.

The recommended strategy is:

```text
freeze semantics
    ↓
protect immutable history
    ↓
normalize canonical identity
    ↓
unify application behavior
    ↓
accelerate projections
    ↓
add part-level retrieval
    ↓
add code provenance
    ↓
add ontology and processors
```

That order preserves the project's strongest idea while preventing advanced features from being built on ambiguous historical truth.

---

# Appendix A — Source map at reviewed commit

Reviewed repository:

```text
https://github.com/ravinsharma7/missis
```

Reviewed commit:

```text
da34b8ae0ad740a7628d2f087f0a44eba00bd532
```

Important source locations:

```text
implementation/store/store.go
    Open
    ensureStoreIdentityAndHashes
    rebuildHashesTx
    CheckConsistency
    RepairSequenceGaps
    appendBatchOnce
    ListTickets

implementation/store/migrations/0001_init.sql
implementation/store/migrations/0002_link_operation_index.sql
implementation/store/migrations/0003_store_identity.sql

implementation/model/model.go
implementation/model/projection.go
implementation/model/validation.go
implementation/model/links.go
implementation/model/markdown.go

pkg/missis/commands.go
pkg/missis/discovery.go

cmd/missis/main.go

testsuite/blackbox/
testsuite/benchmarks/

specs/missues-issue-specification.v2.md
specs/phase1-requirements.md
specs/requirements-registry.v3.json

docs/projects-groups-and-scopes.md
README.md
```

---

# Appendix B — Recommended integrity report shape

```json
{
  "store_id": "store:01...",
  "format_version": 1,
  "verification_mode": "full",
  "started_at": "2026-08-17T01:00:00Z",
  "completed_at": "2026-08-17T01:00:01Z",
  "event_count": 12345,
  "stream_count": 431,
  "computed_head": "abc...",
  "stored_head": "abc...",
  "external_checkpoint": {
    "present": true,
    "head": "abc...",
    "status": "matched"
  },
  "checks": [
    {"name": "event_decode", "status": "pass"},
    {"name": "stream_sequence", "status": "pass"},
    {"name": "canonical_hash_chain", "status": "pass"},
    {"name": "stored_hash_rows", "status": "pass"},
    {"name": "projection_rebuild", "status": "pass"}
  ],
  "status": "verified"
}
```

---

# Appendix C — Recommended processor contract

```go
type ProcessorRequest struct {
    InvocationID InvocationID
    Processor    ProcessorIdentity
    Snapshot     ReadOnlySnapshot
    Inputs       []Ref
    Trigger      EventID
    Capabilities CapabilitySet
    Budget       Budget
}

type ProcessorResult struct {
    ProposedEvents []EventProposal
    Evidence       []EvidenceProposal
    Diagnostics    []Diagnostic
    ObservedEffects []ObservedEffect
}

type Processor interface {
    Process(context.Context, ProcessorRequest) (ProcessorResult, error)
}
```

Required:

```text
Processor cannot commit directly.

Application service validates every proposed event.

Invocation identity, version, code hash, config hash, inputs,
outputs, duration, and diagnostics are recorded.

Cycle and event budgets are enforced.
```

---

# Appendix D — Recommended search query model

```go
type SearchQuery struct {
    Text string

    EffectiveAt time.Time
    KnownAt     time.Time

    Projects []ProjectID
    Groups   []GroupID
    Types    []OntologyRef
    Tags     []string

    CandidateMethods []CandidateMethod
    Relations        []RelationID
    LineageDepth     int

    Limit  int
    Cursor string
}

type SearchResult struct {
    Hits       []PartHit
    Aggregates []TicketAggregate
    Diagnostics SearchDiagnostics
}
```

The search request remains a `show` query at the CLI level:

```bash
missis show --search "retry after cancellation" --project safedesign
```

No fourth domain command is required.
