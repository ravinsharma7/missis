# Current Context

This is a short-lived scratchpad for agents and collaborators. The authoritative
live work items are in the repo-local missis store.

Read this before starting implementation. Then run:

```bash
missis show
```

For the current active project/group/ticket focus, read `.missis.d/active.md`.

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

## Known open areas

- Whole-ticket retraction/hidden projection
- Deterministic sequence-gap root-cause proof
- Backup retention policy
- Fork reconciliation/sync
- Full high-level command orchestration in SDK
- Runtime ontology loading and enforcement
