# Guarantees and performance

**Status:** canonical inventory (2026-08-19). Companion to
`docs/storage-compatibility.md` and the canonical spec
(`specs/missues-issue-specification.v2.md`). The precondition inventory in
this document is authoritative for the store and service layers (tickets #77
and #78).

Guarantees differ by layer. Consumers must not assume a guarantee from one
layer at another. This document states what each layer promises and what it
costs at different scales.

## 0. Terminology

Three distinct terms must not be interchanged:

| Term | What it is | Example |
| --- | --- | --- |
| **Assertion** | A recorded claim that a relation holds between two refs; an `assert-link` event with provenance. The visible relation is derived from assertions (visible while at least one is active). | `set '#184/links' --add 'supports:#12'` |
| **Declaration** | A part under the reserved `schema/` subtree on a scope entity that maps a key prefix to a declared value kind. A meaning rule, not a claim about a relation. | `schema/status -> status` on `project:x` |
| **Precondition** | A write-time guard naming an expected current event; the mutation applies only while the expectation holds, otherwise a conflict. Per-request, never stored. | `--if-current @eN`; `LinkPrecondition` |

Overlap: all three are ledger records, bitemporal, retractable, and
provenance-bearing. Differences: assertions claim relations between refs,
declarations assign meaning to part keys, preconditions constrain when a
write may apply. Assertions and declarations may be retracted; preconditions
cannot be retracted because they are never stored.

## 1. Core guarantees (event model)

| Guarantee | Contract |
| --- | --- |
| Events are immutable | Accepted events are never rewritten, deleted, or repaired in place (spec 10; ticket #41). Recovery from corruption is restore-from-backup. |
| Per-stream sequences | Sequences are unique and strictly increasing per stream; a gap is an integrity incident (ticket #41). |
| Bitemporal winner rule | Latest effective time wins; recorded time breaks ties (spec 10.9; ticket #42). |
| Canonical encoding | Event hashes use canonical encoding v1 (spec 10.10; ticket #45). |
| No destructive delete | The only removal mechanism is retraction (spec 6.9; AGENTS.md). |

## 2. Store guarantees

| Guarantee | Contract |
| --- | --- |
| Batch atomicity | `AppendBatch` and `ApplyLinkBatch` commit all events in one transaction, including across multiple streams (multi-stream batches, ticket #77). A failed batch writes nothing. |
| Idempotency | A repeated append with the same idempotency key replays the stored result and events (ticket #63). |
| Derived tables | `tickets`/`parts_current` are rebuildable from the ledger with parity checks (ticket #51, #61). |
| Precondition form 1 (part/entity) | `Precondition{TargetEntity, ExpectedCurrentEvent}`: the mutation applies only if the part's current event matches (CLI `--if-current`). Mismatch = conflict. |
| Precondition form 2 (link-assertion) | `LinkPrecondition{From, Relation, To, ExpectedCurrentEvent}`: the mutation applies only if the expected event is still an active assertion of that triple (evidence semantics, ticket #66; preconditions from #77). Mismatch = conflict. |
| Hash chain | Append order defines a SHA-256 chain; `show --health` verifies it (ticket #51). |

## 3. Service workflow guarantees

| Workflow | Guarantee |
| --- | --- |
| `NewTicket` | Atomic ticket creation; `--project P` additionally asserts `has-home` in the same batch and fails with guidance when `P` does not exist (spec 14.8). |
| `NewEntity` | Project/group creation rejects an existing canonical ID; repeating a request with the same idempotency key replays the original entity result. |
| CLI scope guard | `new` rejects `--tag project:ID` and `--tag group:ID` before writing a ticket; scope must use `--project` or an explicit link. |
| Markdown import (`--project`) | Ticket, content, and the `has-home` assertion land in one atomic batch; missing targets fail with guidance; reimport never changes membership (spec 14.8; ticket #73). |
| `SetLink` | SDK calls remain additive evidence assertions; targets resolve at write time; relation endpoint rules enforced; has-home uniqueness enforced; last-home retraction warns. The CLI suppresses an already-active identical assertion by default; `--allow-duplicate` opts back into additive behavior. |
| `MoveLink` | Retract + assert in one atomic batch; membership relations only; automatic link-assertion precondition (explicit `IfCurrent` override); unguarded moves rejected; result reports the transition and never emits the zero-home warning (ticket #77). |
| `JoinScope` / `LeaveScope` | Phase 4 scope membership (ticket #74): a `member-of` assertion or retraction (entity to project/group) in one atomic batch with validated targets; leave targets a specific assertion (default retract-all); evidence semantics applies. |
| Markdown import/reimport | All-or-nothing: any violation rejects the whole batch (schema subspec rev 5). |

## 4. SDK guarantees

- Stateless: the SDK carries no hidden context; context is a client-side
  preference (`MISSIS_PROJECT` / `MISSIS_GROUP`), never a model concept.
- Multi-scope filters: `ListFilter` and `SearchOptions` accept typed project
  and group collections; values union within each kind and intersect across
  project and group kinds. Empty scope means all tickets; `Unscoped` selects
  tickets in neither project nor group views and cannot be combined with
  scope values. `CountTicketsFiltered` uses the same semantics. The SDK is
  typed-only; comma-separated parsing belongs to CLI and environment input
  adapters.
- The next pre-1.0 SDK release (`v0.2.0`) is the typed-only scope-filter
  cleanup. Migrate
  `ListFilter.Project` to `ListFilter.Projects`, `ListFilter.Group` to
  `ListFilter.Groups`, and the corresponding `SearchOptions` fields. This is
  an API compatibility change only; no storage or link migration is needed.
- Ticket-view membership uses project `contains`/`has-home`, direct group
  `contains`, and one-hop group `contains`/`governs` project membership.
  Direct and derived paths are unioned and deduplicated by ticket ID;
  `member-of` and generic relations do not affect ticket views in v1.
- Ref-keyed: every read and mutation is addressed by explicit refs
  (ticket, part, project, group, event); resolution is deterministic.
- The SDK facade (`pkg/missis`) is the only public surface; `internal/*` is
  not importable by external consumers.

## 5. Performance at scale

`n` = ledger size. Derived-index work (#70 schema declarations, #75 scope
entity indexes) is the planned remedy for O(n) reads; correctness is
unchanged until then.

| Path | Complexity | Notes |
| --- | --- | --- |
| Append (single batch) | Amortized constant per event (#61) | Sequence allocation + validation of the affected stream. |
| Multi-stream batch | One transaction, per-stream sequences | Used by `MoveLink` for `contains`/`governs` (ticket #77). |
| Link-precondition evaluation | O(n) | Full-ledger scan inside the append transaction until #75. |
| `ListEntities` (projects/groups) | O(n) | Full-ledger scan until #75. |
| `refExists` (link targets) | O(n) or O(stream) | Projects/groups/tickets use stream loads; parts/events scan the ledger. |
| Scope history | O(stream) | Loads one entity stream. |
| Schema declaration resolution | O(n) per write | Until #70. |

Expected gains: #70/#75 derived tables turn the O(n) rows above into indexed
reads, matching the #51 snapshot pattern.

## 6. Precondition inventory (authoritative)

1. **Part/entity expected-current-event** — `Precondition{TargetEntity,
   ExpectedCurrentEvent}`, CLI `--if-current @eN`. Applies to part and entity
   current values.
2. **Link-assertion expected-current-event** — `LinkPrecondition{From,
   Relation, To, ExpectedCurrentEvent}`. Applies to link retraction and
   assertion; used automatically by `MoveLink` (ticket #77). Evidence
   semantics: passes while the expected event is an active assertion of the
   triple (ticket #66).

Preconditions are per-request and never stored; a caller may always pass a
different expected event on the next call.
