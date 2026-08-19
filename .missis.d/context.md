# Current Context

This is a short-lived scratchpad for agents and collaborators. The authoritative
live work items are in the repo-local missis store.

Read this before starting implementation. Then run:

```bash
missis show
```

For the current active project/group/ticket focus, read
`active.local.md` when present, otherwise `active.example.md`, from the same
metadata directory as this file.

## Active focus

- Reusable Go SDK facade: `pkg/missis`
- TUI explorer: `tools/ticket-tui`
- Reliable append/concurrency behavior
- Backup, manifest, and R2 restore loop

## Current local setup

```text
.missis -> ./.missis-store/missis.db
.missis.d/ -> committed manifest and SHOULD backlog
.missis-store/ -> ignored SQLite database
```

Default active project and group are both `none`. Projects and groups are
supported by missis, but this repository is not currently using them as active
scopes. They can be created later and linked to existing tickets at any time.

Project IDs and group IDs are canonical identifiers. Duplicate IDs for the
same entity kind are rejected. A project ID and a group ID may share the same
text because they are different entity kinds.

`active.local.md` is read-only from the command surface. Normal missis commands
must not overwrite it. If context switching becomes a command feature, it must
require an explicit action.

Do not point multiple worktrees or branches at the same SQLite store path.
Each worktree should use its own local `.missis-store/` unless a shared remote
store is explicitly intended.

Run these to verify the current setup:

```bash
missis show --health
go run ./tools/store-gaps .missis-store/missis.db
```

## Recent decisions

- Project metadata lives in `.missis.d/`.
- SQLite store remains ignored under `.missis-store/`.
- Global operational flags are allowlisted, not open-ended.
- No destructive delete; use retraction.
- Sequence gaps are integrity incidents: sequences are immutable, unique,
  strictly increasing; no in-place repair; recovery = restore from backup
  (ticket #41).
- Bitemporal winner rule: latest effective time wins, recorded time breaks
  ties (main spec section 10.9, ticket #42).
- Timestamp canonical form: 9-digit nanoseconds UTC (ticket #45).
- Canonical event encoding v1: main spec section 10.10; reference
  implementation `model.CanonicalEventBytesV1` (ticket #45).
- Store discovery: env outranks repo markers; markers must stay inside the
  repo root; store dirs 0700 and DB 0600 by default (ticket #47).
- main branch is protected: strict CI checks (ci/linux + ci/windows) are
  enforced for admins, so a push to main that fails CI is rejected (2026-08-18,
  ticket #53). v0.1.0 tagged; storage compatibility statement published in
  docs/storage-compatibility.md and cross-referenced from the spec.
- v1 project/group membership landed (2026-08-19, #28 done): spec 14.8 plus
  the `has-home` / `home-of` vocabulary pair (renamed from `home-project`);
  link targets resolve at write time; context is client-side
  (`MISSIS_PROJECT` / `MISSIS_GROUP`, TUI `x` keybinding); the companion doc
  was retired and references repointed to spec section 14. Follow-ups: #66
  evidence semantics, #73 import `--project`, #74 Phase 4 scope ops, #75
  derived scope indexes.
- Atomic link moves landed (2026-08-19, #77 steps 1/1b/2): `MoveLink`
  service workflow (retract+assert in one batch, per-relation origins),
  `LinkPrecondition` in the store (link-assertion guard, in-transaction
  evaluation), multi-stream `AppendBatch`, and the `MoveHome` SDK
  convenience. Semantics documented in specs/link-workflows.subspec.md
  (transient); TUI membership UI and the guarantees doc remain (#77/#78).
- GitHub API merges on protected main hit a GitHub-side quirk: strict required
  checks report "2 of 2 required status checks are expected" even when checks
  are green, and auto-merge stalls. Workaround: delete branch protection,
  merge, re-add it — or merge from the web UI, which is unaffected
  (2026-08-18, observed twice).

## Known open areas

- Whole-ticket retraction/hidden projection
- Deterministic sequence-gap root-cause proof
- Backup retention policy
- Fork reconciliation/sync
- Full high-level command orchestration in SDK
- Runtime ontology loading and enforcement
- Phase 4 scope membership semantics (join-scope/leave-scope runtime, #74)
- Scope read paths are O(ledger); derived entity indexes pending (#75)
- Multistore/worktree navigation and comparison
