# Clean Missis setup for an external project

This guide is the standalone setup handoff. Prefer giving an agent the pinned
command directly; use the immutable raw URL only when a human-readable handoff
is needed. An explicitly supplied URL may be opened directly. Do not search the
web to discover setup instructions.

## Clean bootstrap

Prerequisites: Go 1.26 or newer, an existing target directory, write access,
network access to the pinned GitHub release, and the Go bin directory on PATH.

From the project that should receive Missis, run one command with an immutable
stable tag:

~~~bash
go run github.com/ravinsharma7/missis/tools/paired-install@<stable-tag> \
  --project . --json
~~~

The installer derives the same tag from its module identity, verifies the
release manifest, archive, both binaries, and their shared identity, then runs
the installed `missis` binary by absolute path. It does not use
`latest` implicitly.

PowerShell uses the same command:

~~~powershell
go run github.com/ravinsharma7/missis/tools/paired-install@<stable-tag> `
  --project . --json
~~~

For a URL handoff, replace `<stable-tag>` with the same immutable tag:

~~~text
https://raw.githubusercontent.com/ravinsharma7/missis/<stable-tag>/docs/agent-setup.md
~~~

## Already installed

Initialize or verify an existing project with:

~~~bash
missis --setup --project . --json
~~~

Inspect without creating, migrating, repairing, or configuring the store:

~~~bash
missis --setup --project . --check --json
~~~

A local development pair is deliberately unconfirmed. Permit it explicitly
only for development:

~~~bash
missis --setup --project . --allow-development --json
~~~

## Result states

- `ready`: the release pair, marker, store, health, and explicit scope are confirmed.
- `ready_development`: an explicitly allowed matching development pair is usable but not release-confirmed.
- `not_ready`: a required setup component is absent.
- `failed`: a required check failed; follow the reported corrective action.

Existing `.missis` markers and stores are preserved. Setup never repairs,
cleans, migrates legacy metadata, inserts agent instructions, installs a skill,
or creates a ticket. Unset `MISSIS_STORE` before project setup.

## Removed setup flags

| Removed | Replacement |
| --- | --- |
| `missis --init` | `missis --setup --project .` |
| `missis --start` | `missis --setup --project .` |
| `missis --get-started` | pinned bootstrap command above |

## Agent handoff and continuation

`missis --ag-pointer` prints an optional block for review and insertion
into `AGENTS.md` or a provider-equivalent file. It never writes the file.
After setup, ticket work starts with:

~~~bash
missis --ag-brief
~~~

Use [the legacy migration guide](missis-migration-prompt.md) for old
`.missis.d` metadata and [storage compatibility](storage-compatibility.md)
for backup, restore, artifact migration, garbage collection, and release
compatibility. Neither workflow is part of project setup.
