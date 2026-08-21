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

Missis requires Go 1.26 or newer. The standard installation includes the
domain CLI and the maintenance-tools CLI; install both at the same ref so
their behavior stays aligned.

From a local checkout on Linux, macOS, or WSL, use the repository installer.
It installs both CLIs into a PATH-visible native POSIX directory and does not
silently use a mise-managed `GOBIN` that is absent from `PATH`:

```bash
MISSIS_REF=v0.2.0 bash scripts/install.sh
```

For a convenient published install from a Missis checkout (tracks `latest`):

```bash
MISSIS_REF=latest bash scripts/install.sh
```

For a reproducible published install, pin a tag or commit instead of relying
on `latest`:

```bash
export MISSIS_REF=v0.2.0
bash scripts/install.sh
```

The installer prints the selected directory and verifies native binaries. If
you install manually, `GOBIN` overrides Go's usual `GOPATH/bin` destination;
make the selected directory explicit and keep it on `PATH`:

```bash
export MISSIS_BIN_DIR="$HOME/go/bin"
mkdir -p "$MISSIS_BIN_DIR"
export PATH="$MISSIS_BIN_DIR:$PATH"
GOBIN="$MISSIS_BIN_DIR" go install "github.com/ravinsharma7/missis/cmd/missis@$MISSIS_REF"
GOBIN="$MISSIS_BIN_DIR" go install "github.com/ravinsharma7/missis/tools/missis-tools@$MISSIS_REF"
file "$MISSIS_BIN_DIR/missis" "$MISSIS_BIN_DIR/missis-tools"
```

Verify which version is installed:

```bash
command -v missis
command -v missis-tools
missis --version
missis show --health
missis-tools --help
```

Inside WSL, use the Linux command without `.exe`:

```bash
missis-tools tui
```

Use `missis-tools.exe` only from native Windows PowerShell or Windows
Terminal, with a project and store on a native Windows path. WSL imports the
Windows `PATH` by default, so seeing a Windows Missis directory under
`/mnt/c/...` is expected; the WSL installer must still place the Linux binary
earlier on `PATH` and verify that `command -v missis-tools` resolves it.

Do not open one `.missis-store/missis.db` concurrently from Windows and WSL.
Keep the binary, project filesystem, and SQLite store in the same OS
environment. If the same logical project needs to move between environments,
use Missis backup/remote synchronization instead of sharing the live database.

## Getting started

The fastest path is the CLI's own guide:

```bash
missis --get-started
```

For a new project, use the [local-first agent setup guide](docs/agent-setup.md)
from the checkout when available. It does not require a web search; use an
immutable GitHub tag or commit only when choosing the published install ref.

Or, step by step:

```bash
# Run this from the project that should receive Missis.
cd /path/to/your/project
if [ -f .missis ]; then
  echo "Missis is already initialized; preserving existing state"
else
  missis --init --json
fi

# Verify the store and project context before ticket work.
missis show --health
missis show --context
missis --ag-brief

# First ticket and everyday workflow
missis new "First ticket" --idempotency-key first-ticket --json
missis show 1 --format markdown
missis set 1/status doing
missis set '#1/notes' "some context"

# Correct and remove (append-only; no destructive delete)
missis set '#1/notes' "revised text"            # overwrites current value
missis set '#1/notes' --retract --reason "moved elsewhere"

# Backup, manifest, health, repair
MISSIS_STORE="$PWD/.missis-store/missis.db" \
  missis-tools backup "$PWD/backups/missis.db"
missis-tools manifest
missis-tools gaps .missis-store/missis.db
missis-tools repair .missis-store/missis.db
```

The examples above use the installed `missis-tools` binary. When working from
the Missis checkout itself, use `go run ./tools/missis-tools ...` instead.

For the complete fresh-project, existing-project, PowerShell, and optional
agent-integration flow, use the [local-first agent setup guide](docs/agent-setup.md).

For how tickets, parts, tags, links, projects, and groups relate, see
spec section 14 ([Projects, groups, and scopes](specs/missues-issue-specification.v2.md#14-projects-groups-and-scopes)).

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

`missis --self-update` updates only `missis`. Reinstall `missis` and
`missis-tools` at the same tag or commit when upgrading both commands.

Initialize or inspect a local Missis project:

```bash
missis --init
missis show --health
```

`missis --start` is an alias for `missis --init`; it does not start a daemon.
When `.missis` already exists, `--init` reports `already_initialized` and
preserves the project marker, store, and metadata. Do not pass a different
store path to replace an existing project store.

## Project layout and store discovery

```text
.missis                       marker pointing to the local SQLite store
.missis.d/                    committed project metadata and manifests
.missis-store/                ignored local SQLite database
internal/                     model, store, and application packages
pkg/missis/                   reusable Go SDK facade
cmd/missis/                   command-line binary
tools/                        development and maintenance tools
tools/missis-tools/           consolidated maintenance-tools binary
```

Store permissions are private-by-default on POSIX (0700 directory, 0600 file).
On Windows this is scoped to user-profile ACLs; POSIX mode bits are not
emulated (ticket #55).

## Storage compatibility

See [docs/storage-compatibility.md](docs/storage-compatibility.md) for the
on-disk format contract: forward-only schema migrations, the integrity
guarantees, derived-table behavior, and the no-downgrade policy (ticket #53).

Per-layer guarantees (core, store, service workflows, SDK) and performance at
scale are documented in [docs/guarantees.md](docs/guarantees.md).

Store discovery order:

1. `--store <path>`
2. `MISSIS_STORE`
3. nearest `.missis` marker
4. default user store (`~/.local/share/missis/missis.db` on POSIX)

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
missis-tools gaps <store.db>
missis-tools repair <store.db>
missis-tools manifest
missis-tools remote upload
missis-tools remote download <destination>
```

`missis-tools repair` verifies store consistency and reports sequence gaps. It
never repairs in place: accepted events are immutable, so recovery from gaps means
restoring a backup or creating a new store.

Backup and remote scripts live in `scripts/`.

Install reusable tools globally:

```bash
MISSIS_REF=v0.2.0 bash scripts/install-tools.sh
```

`install-tools.sh` is the maintenance-tools-only compatibility entry point.
For a normal installation of both CLIs, use `scripts/install.sh`. Native
Windows setup can use the equivalent PowerShell script:

```powershell
$env:MISSIS_REF = "v0.2.0"
$env:Path = "$env:LOCALAPPDATA\MissisTools\bin;$env:Path"
.\scripts\install.ps1
```

Omit `MISSIS_REF` for the convenience default of `latest`. Set
`MISSIS_INSTALL_LEGACY_TOOLS=1` to install the compatibility wrappers at that
same ref.

Or install one tool directly:

```bash
GOBIN="$HOME/go/bin" go install github.com/ravinsharma7/missis/tools/missis-tools@v0.2.0
```

The legacy tools remain individually installable during the migration:

```bash
for tool in ticket-tui repair-store store-gaps store-manifest store-backup store-remote; do
  GOBIN="$HOME/go/bin" go install "github.com/ravinsharma7/missis/tools/$tool@v0.2.0"
done
```

To choose a custom executable name, build from a checkout:

```bash
go build -o ./bin/missis-gaps ./tools/store-gaps
go build -o ./bin/missis-repair ./tools/repair-store
go build -o ./bin/missis-tools ./tools/missis-tools
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
missis-tools tui
```

To select a store explicitly:

```bash
missis-tools tui --store /path/to/project/.missis-store/missis.db
```

The TUI is also a maintenance tool, not a public `missis` command. From the
Missis checkout, use `go run ./tools/missis-tools tui`. It can list tickets,
open a ticket, export Markdown, and compare two tickets.

Separate projects using mise can install both binaries from their own
`mise.toml`:

```toml
[tools]
go = "1.26"
"go:github.com/ravinsharma7/missis/cmd/missis" = "v0.2.0"
"go:github.com/ravinsharma7/missis/tools/missis-tools" = "v0.2.0"
```
