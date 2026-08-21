# Ticket TUI Cheatsheet

The TUI (`missis-tools tui`) is keyboard-driven. From a Missis checkout, use
`go run ./tools/missis-tools tui`. Press `?` from a regular view to open the
in-app cheatsheet; inside text prompts it is typed, and
the ticket-filter picker and help view leave it inert. The in-app cheatsheet is
generated from the same key map as the bottom help bar, so it cannot drift from
the actual bindings. This document is the human-readable equivalent.

To select a store explicitly, use `missis-tools tui --store
/path/to/project/.missis-store/missis.db`. Use the Linux binary inside WSL and
the Windows `.exe` only from native Windows terminals. Do not open one SQLite
store concurrently across the WSL and Windows boundary.

## Global keys

| Key | Action |
| --- | --- |
| `q` | Quit regular views; text prompts type `q`, and the ticket-filter picker leaves it inert |
| `ctrl+c` | Hard quit from anywhere |
| `b` | Back on subpages and the ticket-filter picker (`esc` is the cancel/back alias; text prompts type `b`) |
| `?` | Open the cheatsheet from regular views; text prompts type it, and ticket filters/help leave it inert |
| `t` / `p` / `g` | Jump to the tickets / projects / groups list from any page (prompts excepted); on a list you are already on, shows "already on …" |

## Lists

`t` / `p` / `g` switch lists. `j`/`k` or arrows move; `enter` opens.

### Tickets list

```
j/k move | enter open | n create ticket | c/v compare | e export
s stats | m ownership | t/p/g lists | r refresh | x ticket filters | q quit
```

Press `m` on the tickets list to toggle between the compact table and the
ownership table. Ownership mode adds `PROJECT` and `GROUP` columns while
keeping the same ticket selection and active filters. A `—` means the ticket
has no link of that type.

### Projects / groups list

```
j/k move | enter open | n create project|group | f filter tickets | t/p/g lists
r refresh | x ticket filters | q quit
```

Press `f` on a selected project or group to jump directly to its ticket list.
This replaces the active ticket scope with that one project or group. Use `x`
when you want to combine multiple scopes.

Both entity lists show a `MEMBERS` column: a project row shows `N groups · M
tickets` (groups containing it, tickets homed to it); a group row shows `N
projects · M tickets` (contained projects, directly contained tickets).

## Ticket detail

```
j/k scroll | pgup/pgdn page | home/G top/end | t/p/g lists
T edit title | l quick link | e export | r refresh | R refs | b back
```

## Project / group detail

```
j/k scroll | pgup/pgdn page | home/G top/end | t/p/g lists
f filter tickets | l quick link | r refresh | R refs | b back
```

The quick link picker chooses valid project, group, or ticket targets and
handles membership direction automatically. Use `space` to select multiple
targets, then `enter` to apply them together. This lets one ticket receive a
project and one or more groups in one atomic operation. A single unmarked
target can still be applied directly with `enter`. When opened from a ticket,
existing project/group memberships start checked; applying them again is safe
and does not duplicate active links. The detail shows a `members:` summary
line. After a link succeeds or fails, the affected ticket/list keeps a status
notice; press `R` on the detail view to inspect the resulting references.

## Compare and stats

- Compare: `j/k scroll | pgup/pgdn page | home/G top/end | t/p/g lists | b back`
- Stats: `j/k scroll | pgup/pgdn page | home/G top/end | r refresh | t/p/g lists | b back`

Ticket stats include status and age totals plus ownership breakdowns for
projects and groups, including `(no project)` and `(no group)` counts.

## Input prompts

All prompts support a movable cursor: `←`/`→` move by character, `home`/`end`
jump to the ends, typing and `backspace` act at the cursor.

```
enter save | esc cancel | ←/→ cursor | home/end jump | backspace delete
```

Prompts:

| Prompt | Format |
| --- | --- |
| Create project | `project:<id> <Title>` (e.g. `project:blog Blog`) |
| Create group | `group:<id> <Title>` (e.g. `group:eng Engineering`) |
| Create ticket | `<Title>` |
| Edit title (`T` on ticket detail) | text |
| Quick link (`l`) | choose a target with `j`/`k`, then press `enter` |
| Advanced link entry (`a` in the quick picker) | see below |

### Ticket link relations

From a ticket, `l` directly offers home projects and groups. Press `r` to
choose a ticket-to-ticket relation first, then choose the target ticket.
Press `a` from either picker for the full typed form.

```
add|retract <relation>:<ref> [reason]      move project:<id> [reason]
```

Common ticket relations (target kind in angle brackets):

```
blocks:<ticket> · caused-by:<ticket> · duplicates:<ticket>
supports:<ticket> · contradicts:<ticket> · implements:<ticket>
tracks:<ticket> · documents:<ticket> · has-home:<project>
```

`move project:<id>` moves the ticket's home project. The full built-in
vocabulary is `model.ValidRelations()`; per-ticket schema declarations may
restrict targets further.

### Group link relations

From a group, `l` directly offers projects and tickets. From a project, `l`
offers groups and stores the membership in the correct group-to-project
direction.

```
add|retract contains:<project|ticket> [reason]
add|retract governs:<project> [reason]
```

## Ticket filters (`x`)

`p` and `g` are navigation shortcuts: they open the project or group list.
`x` is different: it opens a selectable list of ticket scopes — `(all tickets)`, `(unscoped tickets)`,
every existing project and group, plus `create project…` / `create group…`:

```
j/k move | space toggle | enter apply | n clean | c clear
r refresh | t/p/g lists | b back
```

The picker starts with the current ticket-list scope checked. It shows the
active context, pending draft, standalone counts, and an exact `Draft matches`
count. `space` toggles one project or group without
changing the active list; all tickets and unscoped tickets are exclusive
modes. `enter` applies the draft, `n` starts from a clean all-ticket context,
`c` clears the draft, `r` refreshes entities and counts, and `esc`/`b` cancels.
`q` and `?` are inert in this picker.
Ticket filters are a view filter over the whole ticket list, not a per-ticket
property, and it never creates entities or changes membership links. `f` on a
project/group detail opens the picker with that entity added to the current
draft.

## Membership shortcuts

- Put a ticket into a project: ticket detail → `l` → choose a project.
- Put a ticket into a group: ticket detail → `l` → choose a group.
- Put a project or ticket into a group: project/group detail → `l` → choose
  the target.
- Jump to a scope's tickets: project/group detail → `f`.
