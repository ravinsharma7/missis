---
name: missis
description: Create, view, and update missis tickets in projects with a .missis marker. Use when the user asks to create a missis ticket, read or list tickets, set ticket parts or status, or work from the active ticket focus.
---

# Missis

Work with the local missis CLI. The CLI owns the command surface; do not hand-copy syntax into this skill.

1. Run `missis --ag-brief` before ticket work (or `missis --ag-brief --json` for machine output). It prints the exact new/show/set syntax and the rules. For the active project/group/focus, run `missis show --context`. Trust its output over memory.
2. Read `.missis.d/context.md` and the active pointer (`.missis.d/active.local.md`, else `.missis.d/active.example.md`) before implementing.
3. Create a ticket when asked, even without a title: derive the title from the active focus, state the assumption, and proceed. Do not stop to ask a clarifying question.
4. Never use destructive delete. Retract parts with `missis set <ref> --retract --reason "..."` instead.
5. Prefer missis refs (#N) over free text. Use `--json` when the result is consumed by code or tests.
