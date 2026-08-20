# Ticket TUI Cheatsheet

The TUI (`go run ./tools/ticket-tui`) is keyboard-driven. Press `?` from a
regular view to open the in-app cheatsheet; inside text prompts it is typed, and
the context picker and help view leave it inert. The in-app cheatsheet is
generated from the same key map as the bottom help bar, so it cannot drift from
the actual bindings. This document is the human-readable equivalent.

## Global keys

| Key | Action |
| --- | --- |
| `q` | Quit regular views; text prompts type `q`, and the context picker leaves it inert |
| `ctrl+c` | Hard quit from anywhere |
| `b` | Back on subpages and the context picker (`esc` is the cancel/back alias; text prompts type `b`) |
| `?` | Open the cheatsheet from regular views; text prompts type it, and context/help leave it inert |
| `t` / `p` / `g` | Jump to the tickets / projects / groups list from any page (prompts excepted); on a list you are already on, shows "already on …" |

## Lists

`t` / `p` / `g` switch lists. `j`/`k` or arrows move; `enter` opens.

### Tickets list

```
j/k move | enter open | n create ticket | c/v compare | e export
s stats | t/p/g lists | r refresh | x context | q quit
```

### Projects / groups list

```
j/k move | enter open | n create project|group | t/p/g lists
r refresh | x context | q quit
```

Both entity lists show a `MEMBERS` column: a project row shows `N groups · M
tickets` (groups containing it, tickets homed to it); a group row shows `N
projects · M tickets` (contained projects, directly contained tickets).

## Ticket detail

```
j/k scroll | pgup/pgdn page | home/G top/end | t/p/g lists
T edit title | l links | e export | r refresh | R refs | b back
```

## Project / group detail

```
j/k scroll | pgup/pgdn page | home/G top/end | t/p/g lists
f filter tickets | r refresh | R refs | b back
```

Groups additionally get `l links`. The detail shows a `members:` summary line.

## Compare and stats

- Compare: `j/k scroll | pgup/pgdn page | home/G top/end | t/p/g lists | b back`
- Stats: `j/k scroll | pgup/pgdn page | home/G top/end | t/p/g lists | b back`

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
| Ticket link (`l`) | see below |
| Group link (`l` on group detail) | see below |

### Ticket link relations

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

```
add|retract contains:<project|ticket> [reason]
add|retract governs:<project> [reason]
```

## Context picker (`x`)

`x` opens a selectable list of scopes — `(all tickets)`, `(unscoped tickets)`,
every existing project and group, plus `create project…` / `create group…`:

```
j/k move | space toggle | enter apply | n clean | c clear
r refresh | t/p/g lists | b back
```

The picker shows the active context, pending draft, standalone counts, and an
exact `Draft matches` count. `space` toggles one project or group without
changing the active list; all tickets and unscoped tickets are exclusive
modes. `enter` applies the draft, `n` starts from a clean all-ticket context,
`c` clears the draft, `r` refreshes entities and counts, and `esc`/`b` cancels.
`q` and `?` are inert in this picker.
Context is a view filter over the whole ticket list, not a per-ticket
property, and it never creates entities or changes membership links. `f` on a
project/group detail opens the picker with that entity added to the current
draft.

## Membership shortcuts

- Put a ticket into a project: ticket detail → `l` → `move project:<id>`.
- Put a ticket or project into a group: group detail → `l` →
  `add contains:<ref>`.
- Jump to a scope's tickets: project/group detail → `f`.
