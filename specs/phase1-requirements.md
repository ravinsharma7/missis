# Phase 1 bootstrap requirement register

This file is a temporary bootstrap artifact. It is not a long-term document
store for the project.

Once missis can track its own work, this register will be deleted and the same
requirements will be represented as missis parts and links inside `issues/`.
Until then, this file is the traceability spine for the Phase 1 check.

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
| PH1-CLI-007 | 5.7 | Default coordination statuses are `open`, `doing`, `blocked`, and `done`; avoid status proliferation. |
| PH1-CLI-008 | 5.7 | A blocked ticket has a reason or dependency. |
| PH1-PART-012 | 6.3 | Conventional part names are recognized but remain open-world. |
| PH1-PART-013 | 6.9 | Stored values retain a declared media type or value kind. |
| PH1-REF-004 | 7.4 | Stored links and provenance target canonical `PartID` values. |
| PH1-PRV-004 | 11.6 | Expected external effects are recorded when policy requires them. |
| PH1-DM-001 | 18 | Go data model defines every Phase 1 entity and projection type. |
| PH1-DM-002 | 18 | Projection and validation functions have concrete signatures. |
| PH1-ACC-001 | 29.1, 29.2, 29.3, 29.4, 29.11 | Phase 1 acceptance criteria are mapped to the requirements above. |

## Invariant coverage

Section 20.3 lists eighteen system invariants. They are addressed as follows:

| Invariant | Register mapping |
| --- | --- |
| 20.3-1 append-only history | PH1-EVT-003 |
| 20.3-2 reproducible projection | PH1-PRJ-005 |
| 20.3-3 no hidden plugin mutation | deferred Phase 6 |
| 20.3-4 explicit actor and time | PH1-EVT-001 |
| 20.3-5 stable code coordinate | deferred Phase 2 |
| 20.3-6 retraction preserves existence | PH1-EVT-004 |
| 20.3-7 current state is derived | PH1-PRJ-001 |
| 20.3-8 search is rebuildable | deferred Phase 7 |
| 20.3-9 ontology version explicit | deferred Phase 5 |
| 20.3-10 external effect distinguishable from intention | PH1-PRV-002, PH1-PRV-004 |
| 20.3-11 canonical identity unique | PH1-REF-002, PH1-PART-003 |
| 20.3-12 inverse relations consistent | deferred Phase 2 |
| 20.3-13 one recursive part type | PH1-PART-001 |
| 20.3-14 containment acyclic | PH1-PART-006 |
| 20.3-15 stable part identity | PH1-PART-003, PH1-PART-004 |
| 20.3-16 temporal containment | PH1-PART-006, PH1-PRJ-004 |
| 20.3-17 no implicit hierarchy cascade | PH1-PART-008 |
| 20.3-18 part is not hidden work item | PH1-PART-010 |

## Complete MUST/SHOULD scan

This table records every uppercase `MUST`, `MUST NOT`, `SHOULD`, and
`SHOULD NOT` clause in the canonical specification and states how this round
addresses it.

`MAY` clauses are intentionally not enumerated here. They are optional and do
not block the first implementation slice.

Status values:

- `phase-1` — current Phase 1 check scope.
- `phase-1-should` — current scope, but `SHOULD` and therefore non-blocking.
- `deferred-phase-N` — assigned to a later specification phase.
- `implementation` — storage/persistence decision for the implementation step.

| Norm ID | Strength | Source | Clause | Status | Register mapping |
| --- | --- | --- | --- | --- | --- |
| N001 | MUST NOT | 5.1 | Only `new`, `show`, `set` top-level subcommands. | phase-1 | PH1-CLI-001 |
| N002 | SHOULD | 5.2 | Commands support common JSON, format, actor, and time options. | phase-1-should | PH1-CLI-002 |
| N003 | MUST | 5.2 | System assigns `recorded_at`; caller cannot forge transaction time. | phase-1 | PH1-CLI-003 |
| N004 | SHOULD | 5.7 | Keep coordination status small. | phase-1-should | PH1-CLI-007 |
| N005 | SHOULD | 5.7 | Blocked ticket has reason or dependency. | phase-1-should | PH1-CLI-008 |
| N006 | SHOULD | 5.7 | Avoid dozens of workflow-specific statuses. | phase-1-should | PH1-CLI-007 |
| N007 | MUST | 6.2 | Use one recursive entity type. | phase-1 | PH1-PART-001 |
| N008 | MUST NOT | 6.2 | No separate `Subpart` storage type. | phase-1 | PH1-PART-001 |
| N009 | SHOULD | 6.3 | Recognize conventional part names. | phase-1-should | PH1-PART-012 |
| N010 | MUST NOT | 6.5 | Path is not canonical identity. | phase-1 | PH1-PART-003 |
| N011 | MUST | 6.5 | Move or rename appends provenance events and does not rewrite history. | phase-1 | PH1-PART-004 |
| N012 | SHOULD | 6.6 | Part has zero or one current structural parent. | phase-1-should | PH1-PART-005 |
| N013 | MUST | 6.6 | Containment changes use events. | phase-1 | PH1-PART-006 |
| N014 | SHOULD NOT | 6.8 | Do not assign workflow status to every subpart. | phase-1-should | PH1-PART-010 |
| N015 | SHOULD | 6.9 | Stored value retains declared media type or value kind. | phase-1-should | PH1-PART-013 |
| N016 | MUST NOT | 6.10 | Do not discard unrecognized headings, fields, or descendants. | deferred-phase-3 | Markdown ingestion |
| N017 | MUST NOT | 6.11 | Retracting parent value does not destroy children. | phase-1 | PH1-PART-009 |
| N018 | MUST | 6.11 | Recursive removal appends retraction or containment events. | phase-1 | PH1-PART-009 |
| N019 | SHOULD | 6.11 | Recursive request represented as one atomic event batch. | phase-1-should | PH1-EVT-006 |
| N020 | MUST | 6.12 | Recursive part model preserves all listed hierarchy invariants. | phase-1 | Invariant coverage |
| N021 | MUST | 6.12 | Reject containment cycles. | phase-1 | PH1-PART-006 |
| N022 | SHOULD | 7.3 | Internally use globally unique immutable identifiers. | phase-1-should | PH1-REF-002 |
| N023 | MUST NOT | 7.3 | Rename or move does not change canonical identity. | phase-1 | PH1-REF-002, PH1-PART-004 |
| N024 | SHOULD | 7.3 | Canonical reference resolves independently of current path. | phase-1-should | PH1-REF-002 |
| N025 | SHOULD | 7.4 | Explicit Markdown IDs override generated slugs. | deferred-phase-3 | Markdown ingestion |
| N026 | MUST | 7.4 | Current path resolves to at most one current part in one projection. | phase-1 | PH1-PART-007 |
| N027 | MUST NOT | 7.4 | Stale path does not silently retarget. | phase-1 | PH1-REF-003 |
| N028 | SHOULD | 7.4 | Resolver reports historical path, current moved path, or ambiguity. | phase-1-should | PH1-REF-003 |
| N029 | SHOULD | 7.4 | Stored links and provenance target canonical part IDs. | phase-1-should | PH1-REF-004 |
| N030 | SHOULD | 8.1 | Source reference pinned to immutable repository state. | deferred-phase-2 | Code references |
| N031 | SHOULD | 8.2 | Parser supports canonical structured code-ref form. | deferred-phase-2 | Code references |
| N032 | SHOULD | 8.3 | Prefer symbol or syntax-node identities while retaining range. | deferred-phase-2 | Code references |
| N033 | SHOULD | 8.4 | Branch reference records resolved commit. | deferred-phase-2 | Git references |
| N034 | SHOULD | 8.5 | Code-reference structure is extensible. | deferred-phase-2 | Code references |
| N035 | SHOULD | 9.1 | Provide minimal relation vocabulary and allow ontology extensions. | deferred-phase-2 | Links |
| N036 | SHOULD | 9.3 | Derive declared inverse relations automatically. | deferred-phase-2 | Links |
| N037 | MUST | 9.4 | Link projection indicates origin. | deferred-phase-2 | Links |
| N038 | MUST | 9.5 | Derived links retain source events and processor invocation. | deferred-phase-2 | Links, processors |
| N039 | SHOULD | 9.7 | Lineage queries support selected relation and time policies. | deferred-phase-2 | Links |
| N040 | MUST | 10.2 | Every event contains `recorded_at`. | phase-1 | PH1-EVT-001 |
| N041 | MUST | 10.2 | Every event contains or defaults `effective_at`. | phase-1 | PH1-EVT-001 |
| N042 | SHOULD | 10.2 | Expose `--effective-at` and `--known-at` separately. | phase-1-should | PH1-PRJ-003 |
| N043 | MUST | 10.3 | Timestamps stored in UTC or unambiguous absolute representation. | phase-1 | PH1-EVT-002 |
| N044 | SHOULD | 10.3 | Human output renders selected timezone. | phase-1-should | PH1-EVT-002 |
| N045 | MUST | 10.3 | Text and JSON output uses RFC 3339-compatible timestamps. | phase-1 | PH1-EVT-002 |
| N046 | MUST | 10.3 | Event ordering within stream is deterministic. | phase-1 | PH1-EVT-002 |
| N047 | SHOULD | 10.3 | Use per-stream sequence number. | phase-1-should | PH1-EVT-002 |
| N048 | MUST | 10.4 | Every event has globally unique immutable ID. | phase-1 | PH1-EVT-001 |
| N049 | SHOULD | 10.5 | Use retraction events for removals. | phase-1-should | PH1-EVT-004 |
| N050 | MUST | 10.5 | Legal/privacy erasure leaves auditable tombstone where permitted. | deferred-phase-8 | Hardening |
| N051 | SHOULD | 10.6 | Correction identifies superseded events and reason. | phase-1-should | PH1-EVT-005 |
| N052 | SHOULD | 10.7 | Semantic temporal diff shows domain changes, not only raw events. | phase-1-should | `show` views |
| N053 | SHOULD | 10.8 | Multi-event import or processor output shares batch ID. | phase-1-should | PH1-EVT-006 |
| N054 | SHOULD | 10.8 | Default import behavior is atomic. | deferred-phase-3 | Markdown ingestion |
| N055 | SHOULD | 11.2 | Events support actor, sources, inputs, causes, effects, supersedes, processor invocation, ontologies, and batch. | phase-1-should | PH1-PRV-001 |
| N056 | MUST NOT | 11.3 | Intention, action, effect, observation, and verification are not collapsed. | phase-1 | PH1-PRV-002 |
| N057 | SHOULD | 11.4 | Runs have stable references. | phase-1-should | PH1-PRV-003 |
| N058 | SHOULD | 11.6 | Expected external effects are recorded when policy requires. | phase-1-should | PH1-PRV-004 |
| N059 | SHOULD | 12.1 | Ontologies may validate hierarchy shape and descendants. | deferred-phase-5 | Ontology |
| N060 | MUST | 12.1 | Ontology-defined cascades are deterministic, versioned, explainable, and provenance-bearing. | deferred-phase-5 | Ontology |
| N061 | SHOULD | 12.4 | Ontologies produce explicit obligations. | deferred-phase-5 | Ontology |
| N062 | SHOULD | 12.7 | Verification results are more expressive than Boolean. | deferred-phase-5 | Ontology |
| N063 | SHOULD | 12.8 | Composition is monotonic by default. | deferred-phase-5 | Ontology |
| N064 | MUST | 12.8 | Exemption or override is explicit, scoped, versioned, and provenance-bearing. | deferred-phase-5 | Ontology |
| N065 | SHOULD | 13.1 | Expose deterministic lifecycle. | deferred-phase-6 | Processors |
| N066 | MUST NOT | 13.3 | Processor does not silently mutate authoritative storage. | deferred-phase-6 | Processors |
| N067 | MUST | 13.3 | Subtree processing records root, revisions, policy, limits, and outputs. | deferred-phase-6 | Processors |
| N068 | MUST NOT | 13.3 | Processor does not secretly rewrite descendants. | deferred-phase-6 | Processors |
| N069 | MUST | 13.5 | Effectful processors declare capabilities and record effects. | deferred-phase-6 | Processors |
| N070 | SHOULD | 13.5 | Default policy is least privilege and default deny. | deferred-phase-6 | Processors |
| N071 | SHOULD | 13.6 | Each invocation records provenance. | deferred-phase-6 | Processors |
| N072 | SHOULD | 13.7 | Processors form explicit dataflow or DAG. | deferred-phase-6 | Processors |
| N073 | MUST | 13.7 | Ordering is inspectable and not hidden. | deferred-phase-6 | Processors |
| N074 | SHOULD | 13.8 | Processor idempotency key includes stable inputs. | deferred-phase-6 | Processors |
| N075 | MUST | 13.8 | Hook engine prevents infinite recursion. | deferred-phase-6 | Processors |
| N076 | SHOULD | 13.8 | Prevented cycle produces diagnostic event or visible result. | deferred-phase-6 | Processors |
| N077 | SHOULD | 13.9 | Hook failure creates diagnostic and, where required, obligation. | deferred-phase-6 | Processors |
| N078 | MUST | 13.9 | Retrying processor preserves provenance and avoids duplicate facts. | deferred-phase-6 | Processors |
| N079 | MUST | 14.1 | Relations distinguish organization from semantic authority. | deferred-phase-4 | Projects/groups |
| N080 | SHOULD | 14.4 | Ticket has one home project. | deferred-phase-4 | Projects/groups |
| N081 | MUST | 14.5 | Governance conflicts are deterministic. | deferred-phase-4 | Projects/groups |
| N082 | MUST NOT | 14.7 | Membership alone does not define confidentiality or authorization. | deferred-phase-4 | Projects/groups, security |
| N083 | MUST | 15.1 | CLI and Markdown produce same internal concepts. | deferred-phase-3 | Markdown ingestion |
| N084 | MUST | 15.2 | Plain Markdown works. | deferred-phase-3 | Markdown ingestion |
| N085 | SHOULD | 15.4 | Imported parts retain origin. | deferred-phase-3 | Markdown ingestion |
| N086 | SHOULD | 15.5 | One Markdown import is one atomic batch by default. | deferred-phase-3 | Markdown ingestion |
| N087 | SHOULD | 15.6 | Export preserves hierarchy, canonical identities where configured, and references. | deferred-phase-3 | Markdown ingestion |
| N088 | SHOULD | 15.6 | Round-trip preserves relevant fields without exact whitespace. | deferred-phase-3 | Markdown ingestion |
| N089 | SHOULD | 15.6 | Importer preserves explicit part IDs across export and re-import. | deferred-phase-3 | Markdown ingestion |
| N090 | MUST | 15.6 | Heuristic matches are explainable and require confirmation when ambiguous. | deferred-phase-3 | Markdown ingestion |
| N091 | SHOULD | 15.7 | Hierarchy changes from import are one atomic batch. | deferred-phase-3 | Markdown ingestion |
| N092 | SHOULD | 16.1 | Primary search unit is part revision. | deferred-phase-7 | Search |
| N093 | SHOULD | 16.1 | Nested parts are independently retrievable with breadcrumb. | deferred-phase-7 | Search |
| N094 | MUST NOT | 16.1 | Aggregate search documents do not replace authoritative child values. | deferred-phase-7 | Search |
| N095 | SHOULD | 16.2 | Search architecture is friendly to embedded and remote backends. | deferred-phase-7 | Search |
| N096 | SHOULD | 16.4 | Embedding provenance is recorded. | deferred-phase-7 | Search |
| N097 | SHOULD | 16.6 | Bitemporal search supports effective and known times. | deferred-phase-7 | Search |
| N098 | MUST | 16.6 | Search result respects retraction and supersession. | deferred-phase-7 | Search |
| N099 | SHOULD | 16.10 | `--explain` shows candidate sources and reranking. | deferred-phase-7 | Search |
| N100 | SHOULD | 16.11 | Search supports hierarchy-aware constraints and expansion. | deferred-phase-7 | Search |
| N101 | SHOULD | 16.12 | Asynchronous index exposes watermark or lag. | deferred-phase-7 | Search |
| N102 | MUST | 24.1 | Avoid duplicating authoritative data across views; projections are caches. | implementation | Persistence |
| N103 | SHOULD | 24.4 | Post-commit work uses durable outbox in same transaction. | implementation | Persistence |
| N104 | SHOULD | 24.5 | Large artifacts are content-addressed. | implementation | Persistence |
| N105 | MUST | 18.3 | Current `ParentID` is reconstructable from containment events. | phase-1 | PH1-PART-006, PH1-PART-004 |
| N106 | SHOULD | 20.4 | Use defensive validation at trust boundaries. | phase-1-should | PH1-EVT-008 |
| N107 | SHOULD | 21.1 | `set` supports expected revision for scalar updates. | phase-1-should | PH1-CON-001 |
| N108 | SHOULD | 21.2 | Hierarchy mutations support expected revisions. | phase-1-should | PH1-CON-001 |
| N109 | SHOULD | 21.2 | Subtree move or recursive retraction commits atomically. | phase-1-should | PH1-CON-002 |
| N110 | SHOULD | 21.3 | Independent additions avoid unnecessary conflicts. | phase-1-should | PH1-CON-003 |
| N111 | SHOULD | 21.4 | Batch containing parts, links, and metadata commits atomically. | phase-1-should | PH1-EVT-006 |
| N112 | SHOULD | 21.5 | `new` and `set` accept idempotency keys. | phase-1-should | PH1-CON-004 |
| N113 | SHOULD | 22 | JSON errors are structured. | phase-1-should | PH1-CLI-006 |
| N114 | SHOULD | 23.1 | Plugins, agents, and hooks receive explicit capabilities. | deferred-phase-6 | Security, processors |
| N115 | SHOULD | 23.2 | Effectful capabilities are denied by default. | deferred-phase-6 | Security, processors |
| N116 | SHOULD | 23.3 | Invocation record states granted capabilities. | deferred-phase-6 | Security, processors |
| N117 | MUST | 23.4 | Plugin output is validated as untrusted input. | deferred-phase-6 | Security, processors |
| N118 | SHOULD | 23.5 | Secret values are referenced by opaque credential IDs. | deferred-phase-8 | Security |
| N119 | MUST NOT | 23.6 | Containment alone does not imply authorization inheritance. | deferred-phase-8 | Security |
| N120 | MUST | 23.6 | Recursive search and subtree export check authorization for every returned part. | deferred-phase-8 | Security |
| N121 | MUST | 23.7 | Search candidate generation and reranking apply authorization filters. | deferred-phase-8 | Security |

## Retirement condition

This register is ready to retire when missis can:

- create a ticket from this register;
- represent each requirement as a part;
- link each requirement to its data-model type and test case;
- rerun the Phase 1 check from self-tracked data.
