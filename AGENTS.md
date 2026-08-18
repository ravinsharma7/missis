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
- Markdown exports and anything under `temp/` or `backups/`
- `.missis.d/context.md` — agent scratchpad; authoritative work items live in
  the store

## Ticket lifecycle

- A ticket is `done` only when its `done-when` criteria are met AND no
  follow-up remains. Outstanding work becomes a new ticket linked to the old
  ref; a done ticket must not carry a `next` or `followup` part.
- Verify with `go run ./tools/check-done` (exit non-zero on violations).
- Before creating tickets, list existing refs with `missis show` — alias
  numbers may be allocated by other sessions.
