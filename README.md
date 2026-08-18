# What is missis?

> **Alpha status:** This project is under active development. Features,
> storage behavior, CLI details, and stability are not yet confirmed. Use it
> for experiments, not as a production dependency.

The repository is organized around three core artifacts:

- canonical specifications
- a portable black-box test suite
- a reference implementation

# Introduction

`missis` exposes three domain commands:

```text
missis new
missis show
missis set
```

It also permits a small allowlist of global operational flags for process-level
maintenance or inspection. Those flags must not create new domain workflows.

The event ledger is authoritative. Tickets, parts, links, and views are derived
projections over immutable events.

The repository is intended to be cleanroom-portable: the specs and black-box
suite define the contract, while the reference implementation demonstrates one
concrete implementation.

# Install

From a local checkout:

```bash
go install ./cmd/missis
```

From the published module:

```bash
go install github.com/ravinsharma7/missis/cmd/missis@latest
```

For reproducible installs, pin a tag or commit instead of relying on `latest`:

```bash
go install github.com/ravinsharma7/missis/cmd/missis@v0.1.0
go install github.com/ravinsharma7/missis/cmd/missis@<commit-sha>
```

Go places the binary in `$(go env GOPATH)/bin`. Make sure that directory is on
your `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

Verify which version is installed:

```bash
missis show --version
missis show --health
```

## Getting started

The fastest path is the CLI's own guide:

```bash
missis --get-started
```

Or, step by step:

```bash
# Initialize in your project (creates the .missis marker + .missis.d/ metadata)
cd /path/to/your/project
missis --init
missis show --health

# First ticket and everyday workflow
missis new "First ticket" --json
missis show 1 --format markdown
missis set 1/status doing
missis set '#1/notes' "some context"

# Correct and remove (append-only; no destructive delete)
missis set '#1/notes' "revised text"            # overwrites current value
missis set '#1/notes' --retract --reason "moved elsewhere"

# Backup, manifest, health, repair
MISSIS_STORE="$PWD/.missis-store/missis.db" \
  go run ./tools/store-backup "$PWD/backups/missis.db"
go run ./tools/store-manifest
go run ./tools/store-gaps .missis-store/missis.db
go run ./tools/repair-store .missis-store/missis.db
```

For how tickets, parts, tags, links, projects, and groups relate, see
[docs/projects-groups-and-scopes.md](docs/projects-groups-and-scopes.md).

## Deletion and retraction

`missis` does not support destructive delete. The event ledger is append-only.

To remove a part value from current views:

```bash
missis set '#12/notes' --retract --reason "remove current value"
```

To remove a part and its children from current views:

```bash
missis set '#12/analysis' --retract --recursive --reason "remove subtree"
```

To remove a link:

```bash
missis set '#12/links' --retract "blocked-by:#15"
```

Whole-ticket retraction is not implemented yet. Ticket identity remains in the
event ledger, so do not treat part retraction as a hard delete.

Check for a newer published module:

```bash
missis --self-update-check
```

Update the installed binary:

```bash
missis --self-update
```

Initialize a local missis project:

```bash
missis --init
missis --start
missis --init --store ./.missis-store/missis.db
```

## Project layout and store discovery

```text
.missis                       marker pointing to the local SQLite store
.missis.d/                    committed project metadata and manifests
.missis-store/                ignored local SQLite database
internal/                     model, store, and application packages
pkg/missis/                   reusable Go SDK facade
cmd/missis/                   command-line binary
tools/                        development and maintenance tools
```

Store permissions are private-by-default on POSIX (0700 directory, 0600 file).
On Windows this is scoped to user-profile ACLs; POSIX mode bits are not
emulated (ticket #55).

Store discovery order:

1. `--store <path>`
2. nearest `.missis` marker
3. `MISSIS_STORE`
4. XDG fallback

Committed metadata paths are configurable:

```bash
MISSIS_MANIFEST_PATH
MISSIS_SHOULD_BACKLOG_PATH
MISSIS_PHASE1_REQUIREMENTS_PATH
```

## Go SDK

The public facade is:

```go
import "github.com/ravinsharma7/missis/pkg/missis"
```

It provides store discovery, a `Client` wrapper, event helpers, health,
backup, and verification methods. `cmd/missis` is intended to stay a thin CLI
layer.

## Maintenance tools

```bash
go run ./tools/store-gaps <store.db>
go run ./tools/repair-store <store.db>
go run ./tools/store-manifest
```

`repair-store` verifies store consistency and reports sequence gaps. It never
repairs in place: accepted events are immutable, so recovery from gaps means
restoring a backup or creating a new store.

Backup and remote scripts live in `scripts/`.

Install reusable tools globally:

```bash
bash scripts/install-tools.sh
```

Or install one tool directly:

```bash
go install github.com/ravinsharma7/missis/tools/ticket-tui@latest
go install github.com/ravinsharma7/missis/tools/repair-store@latest
go install github.com/ravinsharma7/missis/tools/store-gaps@latest
go install github.com/ravinsharma7/missis/tools/store-manifest@latest
go install github.com/ravinsharma7/missis/tools/store-backup@latest
```

# Viewing tickets

`show` reads the event ledger and derives the current projection on demand. It
does not require exporting Markdown or writing files.

```bash
# list tickets
missis show

# show one ticket's current part tree
missis show '#16'

# show one virtual part directly
missis show '#16/analysis/no-pointer/pros'

# machine-readable projection
missis show '#16' --json

# optional Markdown export
missis show '#16' --format markdown
```

For an interactive terminal UI, use:

```bash
go run ./tools/ticket-tui
```

The TUI is also a development tool, not a public `missis` command. It can list
tickets, open a ticket, export Markdown, and compare two tickets.
