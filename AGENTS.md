AG1: keep things relevant.  
AG2: don't leave things open ended.  
AG3: mention when something is confirmed, not confirmed, unknown, or not sure.  
AG4: propose "hill climbing" solutions when it assists long term stability of the project.  

## Document authority

Authoritative contracts:
- `specs/missues-issue-specification.v2.md`
- `specs/phase1-requirements.md`
- `specs/requirements-registry.v3.json`
- the repo-local missis store (`.missis-store/`), including tickets

Transient or generated context (treat as context, never as contract):
- `reports/*` — audit and analysis outputs
- `*.subspec.md` — working drafts, merged into the main spec then deleted
- Markdown exports and anything under `temp/` or `.missis-backups/`
- legacy `.missis.d/` context/pointer files — optional historical metadata;
  authoritative work items live in the store and optional scope defaults come
  from explicit `MISSIS_PROJECT`/`MISSIS_GROUP` values
- legacy `.missis.d/*backlog*`, and store-side exports — generated or planning
  metadata, never task direction or contract; reviewed Phase 1 decisions live
  in `specs/requirements-registry.v3.json`
- Store manifests are generated on demand from the live database by
  `missis-tools manifest`; no committed manifest snapshot is authoritative.

## Ticket lifecycle

- A ticket is `done` only when its `done-when` criteria are met AND no
  follow-up remains. Outstanding work becomes a new ticket linked to the old
  ref; a done ticket must not carry a `next` or `followup` part.
- When a `done-when` criterion cannot be satisfied inside the ticket (for
  example, a manual check that needs an operator), re-scope the done-when to
  the achieved state and move the unmet criterion to a new follow-up ticket
  linked via `related`. Never mark a ticket `done` with an unchecked or
  skipped criterion; make the outstanding work explicit and owned.
- Verify with `go run ./tools/check-done` (exit non-zero on violations).
- Before creating tickets, list existing refs with `missis show` — alias
  numbers may be allocated by other sessions.
