# Link workflows - atomic moves and link-assertion preconditions

**Status:** working subspec draft, rev 1 (2026-08-19). Transient, not merged,
not normative. Merge into the main spec after hardening (see
`## Merge checklist`); the repo convention is to delete the subspec file once
merged.

**Tracked by:** ticket #77 (Add TUI membership and move management).
Complements `docs/guarantees.md` (ticket #78) and the v1 membership contract
in spec 14.8.

## Motivation

Membership changes were CLI-only and composed of two independent link
operations (retract then assert). That is non-atomic (a crash can leave a
zero-home ticket), emits a spurious zero-home warning on the retract, and
offers no concurrency protection. This subspec defines the atomic workflow
(`MoveLink`) and the store-level guard it relies on (link-assertion
precondition), assuming multiple sessions/users from the start.

## MoveLink workflow

`MoveLink(ctx, req, {Relation, From, To, Target, Reason, IfCurrent})` moves an
active membership assertion so its origin changes from `From` to `To` while
the other endpoint stays `Target`. It appends `retract-link` + `assert-link`
in one atomic `AppendBatch`.

The assertion origin depends on the relation:

| Relation | Retracted assertion | Asserted assertion | Events live on |
| --- | --- | --- | --- |
| `has-home` | `(Target, has-home, From)` | `(Target, has-home, To)` | Target stream |
| `contains` | `(From, contains, Target)` | `(To, contains, Target)` | From and To streams |
| `governs` | `(From, governs, Target)` | `(To, governs, Target)` | From and To streams |

Validation (all before append; nothing written on failure):

- relation must be a membership relation in v1 (`has-home`, `contains`,
  `governs`);
- `From`, `To`, and `Target` must resolve to existing refs;
- `From` and `To` must differ;
- endpoint rules per relation (`has-home`: ticket target, project source and
  destination; `contains`: scope source/destination; `governs`: group
  source/destination, project target);
- an active assertion `(origin, relation, other endpoint)` must exist, else
  "nothing to move" with guidance;
- a caller-supplied `IfCurrent` alias must match the current assertion event,
  otherwise a conflict with retry guidance.

Result: a `move-link` operation result reporting the transition
(`has-home:project:a->project:b`) and the last event alias. `MoveLink` never
emits the zero-home warning: the intermediate state does not exist.

## Link-assertion precondition

The store `Precondition` gains a link form:

```text
LinkPrecondition{From, Relation, To, ExpectedCurrentEvent}
```

Semantics: the batch applies only if the current active assertion of
`(From, Relation, To)` is `ExpectedCurrentEvent`; otherwise the append fails
with a conflict. Set semantics apply until evidence semantics (#66): one
active assertion per triple, retraction hides it. The part/entity form
(`TargetEntity` + `ExpectedCurrentEvent`) is unchanged.

`MoveLink` attaches the link precondition automatically: the service reads
the current assertion event at effective time and passes it; callers may
override via `IfCurrent`; an unguarded move is rejected with guidance rather
than silently proceeding.

## Multi-stream batches

`AppendBatch` now accepts events on multiple streams in one transaction:

- events are grouped by stream, sequences allocated per stream;
- ticket alias allocation stays single-stream (`AppendTicketBatch`);
- derived ticket tables are updated per ticket stream in the batch;
- link preconditions are evaluated against the full ledger inside the
  transaction, so the guard is race-free with the append.

This is what makes cross-stream moves (`contains`/`governs`) atomic.

## Guarantees and performance

- `MoveLink` guarantees: atomic (all-or-nothing), guarded (current assertion
  must match), validated (targets resolve, endpoint rules), no spurious
  warning.
- Precondition evaluation loads the full ledger per append
  (O(ledger)); correct, unbounded per-operation cost until #75 lands derived
  indexes. The guarantees inventory lives in `docs/guarantees.md` (#78).

## Boundaries

- v1 moves are restricted to membership relations; arbitrary relations
  (e.g., `supports`) have no defined move semantics yet.
- CLI `--move-home` is deferred (undecided); a future flag maps onto
  `MoveLink`.
- A general link batch-mutation primitive is deferred until a second
  consumer needs multi-op link edits.
- #66 changes link *projection* (evidence semantics); `MoveLink` composes
  existing `retract-link`/`assert-link` events and is unaffected.

## Decision log

| Decision | Rationale |
| --- | --- |
| `MoveLink` is a service workflow, not a core event | Membership stays `assert-link`/`retract-link` (spec 14.8.3, #44); no new operation or projection rule. |
| Link precondition lives in the store | The guard must be inside the append transaction to be race-free. |
| Automatic guard by default, explicit override | Multi-user assumption: every move is guarded; callers that know the alias can pin it. |
| Unguarded moves rejected | Avoids ambiguous last-writer-wins behavior. |
| Multi-stream batches | Cross-stream moves (`contains`, `governs`) need one transaction to be atomic. |
| No zero-home warning from moves | The intermediate state never exists; standalone retractions keep the warning. |

## Acceptance criteria

- [ ] `MoveLink` appends retract+assert atomically for all three membership
      relations; same-target rejected; missing targets rejected with
      guidance; no active assertion rejected with guidance.
- [ ] The store enforces the link-assertion precondition: matching event
      passes, stale event conflicts.
- [ ] `MoveHome` (SDK convenience) moves a ticket's home in one batch without
      warning.
- [ ] Cross-stream `contains` move lands both events and updates both
      project views atomically.
- [ ] Service and store tests cover success, validation, conflict, and
      no-warning paths; `go test ./...` and `go run ./tools/check-done` exit 0.

## Implementation status (transient)

Implemented 2026-08-19 (steps 1, 1b, 2 of #77):

- store: `LinkPrecondition`, multi-stream `AppendBatch`, in-transaction
  precondition evaluation;
- service: `MoveLink` with per-relation origin/endpoint rules and automatic
  guard;
- SDK: `MoveLink` + `MoveHome` convenience;
- tests: store (`TestAppendBatchMultiStream`, `TestLinkPrecondition`),
  service (`TestMove*`), SDK (`TestMoveHomeConvenience`); full suite green.

Not yet implemented: TUI membership UI (#77 step 2), guarantees doc (#78),
and the merge below.

## Merge checklist

Do not merge into the main spec until:

- [ ] Cross-checked against #66 (evidence semantics), #43 (canonical link
      identity), #44 (operation registry), and #78 (guarantees doc).
- [ ] The TUI membership UI (#77 step 2) is implemented and the guarantees
      doc (#78) includes the precondition inventory.
- [ ] Then: merge into spec 9/14.8 plus a new concurrency/precondition
      subsection, update `docs/guarantees.md` cross-references, and delete
      this file.
