---
name: missis
description: Create, view, and update missis tickets in projects with a .missis marker. Use when the user asks to create a missis ticket, read or list tickets, set ticket parts or status, or work from the active ticket focus.
---

# Missis

Work with the local missis CLI. The CLI owns the command surface; do not hand-copy syntax into this skill.

1. Run `missis --ag-brief` before ticket work (or `missis --ag-brief --json` for machine output). It prints the exact new/show/set syntax and the rules. For the active project/group/focus, run `missis show --context`. Trust its output over memory.
2. Read `.missis.d/context.md` and the active pointer (`.missis.d/active.local.md`, else `.missis.d/active.example.md`) before implementing.
3. Before creating a requested project or group, preflight the exact canonical ID with `missis show project:<id>` or `missis show group:<id>`. Create only when absent.
4. Create projects/groups with `missis new --kind project|group --id <id> "<title>"`; do not encode a group as a ticket tag. `--project <id>` sets a ticket's home project; group membership is a `contains` link from `group:<id>` to the ticket.
5. Give every logical create/mutation a stable `--idempotency-key` and reuse it after an uncertain result. Never retry a creation with a fresh key until the store has been checked.
6. Create a ticket when asked, even without a title: derive the title from the active focus, state the assumption, and proceed. Do not stop to ask a clarifying question.
7. Verify the returned ref and requested project/group views before reporting success. Do not use web search for this local workflow unless the user explicitly asks for external research.
8. Never use destructive delete. Retract parts with `missis set <ref> --retract --reason "..."` instead.
9. Prefer missis refs (#N) over free text. Use `--json` when the result is consumed by code or tests.
10. Shells treat `#` as a comment: quote refs in commands (`missis show '#55'`) or use bare numbers (`missis show 55`); an unquoted `#55` silently drops the ref and following flags.
