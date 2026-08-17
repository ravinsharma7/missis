# Projects, groups, and scopes

Companion to the canonical spec. It clarifies the relationships between
tickets, parts, tags, links, projects, and groups. A machine-readable
requirements registry (see `specs/requirements-registry.v3.json`) will carry
the corresponding anchors; this page is the human-readable version.

## Tickets vs parts

Tickets and parts are **different entity kinds**, not the same thing in
different sizes.

- A **ticket** is a canonical work item with its own immutable event stream,
  its own identity, and its own projection. `show '#1'` reads one ticket's
  ledger.
- A **part** is a nested element *inside one ticket*: notes, evidence,
  decisions, structure. Parts are recursive (a part may have child parts;
  there is one `Part` entity, never a separate `Subpart` type), and every part
  belongs to exactly one ticket.

Therefore:

- "Part of a part" is supported: nested child parts inside the same ticket
  (e.g. `#1/evidence/race-test`).
- A ticket is **not** a part, and a part is **not** a ticket. There is no
  entity-kind migration to promote a part into a ticket today.
- A "sub-ticket" as an entity is **unsupported by design**: work decomposition
  across tickets uses typed links between tickets (PH1-PART-010), not hidden
  subpart workflows.
- A part may reference another ticket as a value or via links, but that is a
  relation, not containment.

## Tags

Tags are flat labels stored on a ticket (`part/tag`). They classify a ticket
but carry no structure, parent, or governance semantics. Use tags for
cross-cutting labels such as `cleanup` or `test-artifact`.

## Links

Links are typed relations between any two references (ticket, part, project,
group, event, ...). The current vocabulary includes:

| Relation | Inverse |
| --- | --- |
| `blocks` | `blocked-by` |
| `caused-by` | `causes` |
| `duplicates` | `duplicated-by` |
| `supports` | `supported-by` |
| `contradicts` | `contradicted-by` |
| `implements` | `implemented-by` |
| `tracks` | `tracked-by` |
| `documents` | `documented-by` |
| `contains` | `contained-by` |
| `governs` | `governed-by` |
| `home-project` | `home-of` |

Links are the mechanism for cross-ticket dependencies (`#5 blocked-by #20`),
decomposition across tickets (`#34 contains #35`), and scoping (`#38 tracks
#15`).

## Projects and groups

Projects and groups are canonical entities, like tickets, but they are
**scope containers**, not work items.

- **Project** — a scoped collection with a home relationship (`home-project` /
  `home-of`). Phase 4 of the spec adds: home project, multiple memberships,
  scoped aliases, project views, and temporal membership.
- **Group** — a named collection that can hold or govern other entities.

The relation vocabulary already distinguishes the two kinds of scoping:

- `contains` / `contained-by` — structural membership (what belongs to what).
- `governs` / `governed-by` — authority or policy (what has power over what).

### Current status

Confirmed: refs resolve for `project:` and `group:` kinds, the relation
vocabulary exists, and the model has join/leave scope operations. This
repository runs with project `none` and group `none`; CLI/TUI navigation over
projects and groups is not implemented yet (ticket #28).

### Choosing a mechanism

| Mechanism | Models | Use when | Status |
| --- | --- | --- | --- |
| Ticket | An independently tracked work item | Something needs its own lifecycle, status, and history | Implemented |
| Part | Nested decomposition inside one ticket | Structure, evidence, notes within a ticket | Implemented |
| Tag | Flat classification | Simple cross-cutting labels | Implemented |
| Link | Typed relation between refs | Dependencies, decomposition across tickets, scoping | Implemented (typed links, lineage, retraction) |
| Project | Scope container with home/governance semantics | Grouping a body of work; Phase 4 semantics | Model + refs exist; navigation open (#28) |
| Group | Named collection (contain/govern) | Collections with authority semantics | Model + refs exist; navigation open (#28) |

## Open work

- CLI/TUI navigation and views for projects/groups: #28.
- Temporal membership, scoped aliases, and project/group views: spec Phase 4.
- Backporting this clarification into the canonical spec as machine-readable
  anchors: tracked with the requirements registry work (see
  `specs/requirements-registry.v3.json`).
