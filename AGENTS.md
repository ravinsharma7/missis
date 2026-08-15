AG1: keep things relevant.
AG2: don't leave things open ended.
AG3: mention when something is confirmed, not confirmed, unknown, or not sure.
AG4: use missis to track its own issues in .missis.d/ as a self-tracking and dog-fooding mechanism.
AG5: propose "hill climbing" solutions when it assists long term stability of the project.
AG6: read the configured project context file (default .missis.d/context.md) and current missis tickets before starting implementation work.
AG7: read .missis.d/active.local.md when present, otherwise .missis.d/active.example.md, to determine the active project, group, and ticket focus for this session.

## missis quick reference

Run `missis --agent-brief` once before ticket work. It prints the exact
new/show/set syntax, the active focus, and the rules from the CLI itself; do
not copy that syntax into this file.

When asked to create a ticket without a title, derive one from the active
focus and state the assumption; do not block on a clarifying question.
