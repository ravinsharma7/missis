# Phase 1 bootstrap requirement register

This file is a temporary bootstrap artifact. It is not a long-term document
store for the project.

Once missis can track its own work, this register will be deleted and the same
requirements will be represented as missis parts and links inside `issues/`.
Until then, this file is the traceability spine for the implementation gate.

Canonical specification:

- `specs/missues-issue-specification.v2.md`

Public command spelling:

- `missis new`
- `missis show`
- `missis set`

## Requirement list

| ID | Source | Requirement |
| --- | --- | --- |
| PH1-CLI-001 | 5.1, 5.2 | `missis` exposes only `new`, `show`, and `set` as top-level subcommands. |
| PH1-CLI-002 | 5.2 | Commands support `--json`, `--format text\|json\|markdown`, `--actor`, and `--effective-at`. |
| PH1-CLI-003 | 5.2 | `recorded_at` is system-assigned; callers cannot forge transaction time. |
| PH1-CLI-004 | 5.3 | No operation depends on an implicit current ticket. |
| PH1-CLI-005 | 5.4, 5.5, 5.6 | `new`, `show`, and `set` cover identity creation, projection, and Phase 1 mutations. |
| PH1-CLI-006 | 22 | Exit classes and structured JSON errors are part of the stable contract. |
| PH1-PART-001 | 6.1, 6.2, 20.3-13 | There is one recursive `Part` entity; no separate `Subpart` entity. |
| PH1-PART-002 | 6.1 | A part may have a value only, children only, or both. |
| PH1-PART-003 | 6.5, 7.3 | Every part has a stable canonical ID independent of its human-readable path. |
| PH1-PART-004 | 6.5, 6.6 | Rename and move preserve canonical identity and historical paths. |
| PH1-PART-005 | 6.6, 6.12-1 | A part has zero or one current structural parent. |
| PH1-PART-006 | 6.6, 6.12-3 | Structural containment is temporal, provenance-bearing, and acyclic. |
| PH1-PART-007 | 6.12-9, 7.4 | Paths are unique within one ticket and selected time projection. |
| PH1-PART-008 | 6.7, 6.12-7 | No value, type, status, or permission cascades by default without explicit ontology/policy. |
| PH1-PART-009 | 6.11 | Parent value retraction does not retract descendants; recursive removal is explicit and atomic. |
| PH1-PART-010 | 6.8, 6.12-8 | Work decomposition uses linked tickets, not hidden subpart workflows. |
| PH1-PART-011 | 7.4 | Part path segments use the recommended syntax and resolve under the selected projection. |
| PH1-REF-001 | 7.1, 18.1 | `Ref` covers ticket, part, event, project, group, run, code, git, and artifact. |
| PH1-REF-002 | 7.2, 7.3 | Human-readable references are aliases; logical and exact event references remain distinct. |
| PH1-REF-003 | 7.4 | Stale paths must not silently retarget to a different part. |
| PH1-EVT-001 | 10.2, 10.4, 20.3-4 | Every accepted event has a unique immutable ID, actor, `recorded_at`, and `effective_at`. |
| PH1-EVT-002 | 10.3, 20.2 | Timestamps use unambiguous absolute storage and RFC 3339 output; per-stream sequence is monotonic. |
| PH1-EVT-003 | 20.3-1 | Accepted historical events are append-only and immutable. |
| PH1-EVT-004 | 10.5, 20.3-6 | Retraction removes current state without erasing historical queryability. |
| PH1-EVT-005 | 10.6 | Corrections identify the events they supersede. |
| PH1-EVT-006 | 10.8, 21.4 | Multi-event batches are atomic by default. |
| PH1-EVT-007 | 19 | Internal event operation vocabulary drives projection and validation without new CLI commands. |
| PH1-EVT-008 | 20.1, 20.2 | Append preconditions and postconditions are enforced before durability success. |
| PH1-PRJ-001 | 4.4, 20.3-7 | Current ticket state is a derived projection; the event ledger is authoritative. |
| PH1-PRJ-002 | 4.4, 10.2 | Valid-time projection is supported. |
| PH1-PRJ-003 | 4.4, 10.2 | Bitemporal projection is supported. |
| PH1-PRJ-004 | 6.6, 7.4 | Part path resolution is temporal and projection-scoped. |
| PH1-PRJ-005 | 20.3-2 | Same events, ontology versions, and projection rules produce the same state. |
| PH1-PRV-001 | 11.2 | Events support actor, sources, inputs, causes, effects, supersedes, ontology versions, and batch ID. |
| PH1-PRV-002 | 11.3 | Intention, action, effect, observation, and verification are distinct provenance facts. |
| PH1-PRV-003 | 11.4 | Agent and process runs have stable references. |
| PH1-CON-001 | 21.1, 21.2 | Scalar and hierarchy updates support expected-revision checks. |
| PH1-CON-002 | 21.2, 20.1 | Concurrent hierarchy mutations are atomic and reject cycles or path collisions. |
| PH1-CON-003 | 21.3 | Independent additions may commute without unnecessary conflict. |
| PH1-CON-004 | 21.5 | Clients can use idempotency keys. |
| PH1-DM-001 | 18 | Go data model defines every Phase 1 entity and projection type. |
| PH1-DM-002 | 18 | Projection and validation functions have concrete signatures. |
| PH1-ACC-001 | 29.1, 29.2, 29.3, 29.4, 29.11 | Phase 1 acceptance criteria are mapped to the requirements above. |

## Retirement condition

This register is ready to retire when missis can:

- create a ticket from this register;
- represent each requirement as a part;
- link each requirement to its data-model type and test case;
- rerun the implementation gate from self-tracked data.
