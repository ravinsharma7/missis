# URL-first Missis setup for a new project

You are setting up Missis in the current project directory. Read this guide
from the supplied GitHub URL, then follow the required steps in order. Do not
assume that the current project is a checkout of the Missis repository.

This is a project bootstrap guide. It is not a ticket, and it does not create
a sample ticket. After setup, the agent can continue using the project's own
context and Missis tickets.

## URL and reproducibility

The human-readable form is:

```text
https://github.com/ravinsharma7/missis/blob/<ref>/docs/agent-setup.md
```

The plain-text form is:

```text
https://raw.githubusercontent.com/ravinsharma7/missis/<ref>/docs/agent-setup.md
```

Use an immutable tag or commit for `<ref>` when reproducibility matters. The
`main` branch is convenient for discovery but can change over time. Both Missis
binaries installed below should use the same tag or commit when that ref is
published as a Go module version.

## Prerequisites

Required:

- Access to GitHub or another way to read this guide and download the Missis
  module.
- A shell or terminal running in the project that should receive Missis.
- Permission to write the current project directory and create its local
  Missis metadata and store.
- Go 1.26 or newer for the published-module installation path below.
- Network access for the first Go module download.

The target project may be empty, may already contain source code, or may
already have Missis initialized. The setup must inspect that state and must
not delete or rebuild existing project data.

Optional integrations, such as a provider skill directory or `AGENTS.md`,
require separate permission to modify those locations. They are not required
for the project setup to succeed.

## Requirements

The required setup must:

- Install an explicit Missis tag or commit rather than using `@latest` as the
  reproducible path.
- Operate on the current project directory. Do not clone the Missis repository
  into the target project merely to perform setup.
- Create or verify the `.missis` marker, its referenced store, and the
  `.missis.d/` context metadata. A fresh project uses
  `.missis-store/missis.db`.
- Preserve an existing marker, store, context file, active pointer, and ticket
  data.
- Verify the installed binary, store health, project context, and agent brief.
- Avoid secrets, hidden conversation state, machine-specific absolute paths,
  destructive cleanup, and automatic edits to unrelated project instructions.

## Required setup: POSIX shell

The following example targets the `v0.2.0` release. If this guide was
opened at another immutable tag or commit, replace `v0.2.0` with the matching
published ref before running the install command.

Run the install commands outside the target project if you prefer, then
return to the target project for initialization:

```bash
export MISSIS_REF=v0.2.0
go install "github.com/ravinsharma7/missis/cmd/missis@$MISSIS_REF"
go install "github.com/ravinsharma7/missis/tools/missis-tools@$MISSIS_REF"
export PATH="$(go env GOPATH)/bin:$PATH"

command -v missis
command -v missis-tools
missis --version
missis-tools --help
```

Confirm the current directory is the intended target, then initialize it:

```bash
pwd

fresh=0
if [ -f .missis ]; then
  echo "Missis is already initialized; preserving existing state"
else
  fresh=1
  missis --init --json
fi

test -f .missis
test -f .missis.d/context.md
if [ "$fresh" -eq 1 ]; then
  test -f .missis-store/missis.db
fi
if [ -f .missis.d/active.local.md ]; then
  active_pointer=.missis.d/active.local.md
elif [ -f .missis.d/active.example.md ]; then
  active_pointer=.missis.d/active.example.md
else
  echo "No active pointer found in .missis.d" >&2
  exit 1
fi

missis show --health --json
missis show --context
missis --ag-brief
echo "active pointer: $active_pointer"
```

For a fresh project, expect an `initialized` JSON status, the marker and local
database paths above, generated context metadata, a successful health check,
project context output, and the agent-facing command brief. For an existing
project, expect the marker and referenced store to be preserved; the health
check verifies that store instead of assuming the default local path.

If `.missis` already exists, do not pass a different store path to try to
replace it. Run the verification commands instead:

```bash
missis show --health --json
missis show --context
missis --ag-brief
```

An existing `.missis.d/context.md`, `active.local.md`, or
`active.example.md` is project context. Read and preserve it. Prefer
`active.local.md` when both active pointers exist. The event store is
authoritative for tickets; use `missis show` to inspect it. If required context
metadata is missing, stop and report the exact missing path rather than
reinitializing or overwriting project files.

## Required setup: PowerShell

For Windows PowerShell, use the corresponding commands:

```powershell
$env:MISSIS_REF = "v0.2.0"
go install "github.com/ravinsharma7/missis/cmd/missis@$env:MISSIS_REF"
go install "github.com/ravinsharma7/missis/tools/missis-tools@$env:MISSIS_REF"
$env:Path = "$(go env GOPATH)\bin;$env:Path"

Get-Command missis
Get-Command missis-tools
missis --version
missis-tools --help
Get-Location
```

Initialize only when the current project does not already contain a `.missis`
marker:

```powershell
$fresh = $false
if (Test-Path .missis) {
    Write-Host "Missis is already initialized; preserving existing state"
} else {
    $fresh = $true
    missis --init --json
}

if (!(Test-Path .missis)) { throw ".missis marker is missing" }
if (!(Test-Path .missis.d\context.md)) { throw "Missis context is missing" }
if ($fresh -and !(Test-Path .missis-store\missis.db)) { throw "Missis store is missing" }
if (Test-Path .missis.d\active.local.md) {
    $activePointer = ".missis.d\active.local.md"
} elseif (Test-Path .missis.d\active.example.md) {
    $activePointer = ".missis.d\active.example.md"
} else {
    throw "No active pointer found in .missis.d"
}

missis show --health --json
missis show --context
missis --ag-brief
Write-Host "active pointer: $activePointer"
```

## Local-checkout installation alternative

If Go cannot download the published module but a Missis checkout is already
available, build or install from that checkout instead. Run this from the
Missis checkout, not from the target project:

```bash
go install ./cmd/missis
go install ./tools/missis-tools
```

Then put the resulting Go bin directory on `PATH`, return to the target
project, and follow the required initialization and verification steps above.
This alternative still requires a compatible Go toolchain.

`missis --self-update` updates only the domain CLI. When upgrading a project,
install `missis` and `missis-tools` again with the same tag or commit so the
two binaries remain aligned.

## Optional mise setup

A separate project that already uses mise can install both Go CLIs from its
own `mise.toml`:

```toml
[tools]
go = "1.26"
"go:github.com/ravinsharma7/missis/cmd/missis" = "v0.2.0"
"go:github.com/ravinsharma7/missis/tools/missis-tools" = "v0.2.0"
```

## Optional agent integrations

Project setup is complete without these integrations.

If the agent provider supports skills and a Missis checkout is available, an
explicit checkout path can be installed without overwriting an existing
skill:

```bash
missis --ag-install-skill \
  --from /path/to/missis/tools/skills/missis \
  --dest /path/to/provider/skills/missis
```

Only add `--force` after explicitly authorizing replacement of that skill.
If the provider does not support skills, keep this guide URL or its copied
instructions as the agent handoff context.

If the project uses `AGENTS.md`, review the generated pointer before adding
it. Do not replace unrelated project instructions:

```bash
missis --ag-pointer
```

## Completion report and continuation

Before continuing, report:

- The target project directory.
- The installed Missis version and ref.
- The `.missis` marker and store paths.
- The health-check result.
- Whether `.missis.d/context.md` and the active pointer were created or
  preserved.
- Whether optional skill or `AGENTS.md` integration was installed.

For the next agent turn, read `.missis.d/context.md` and the active pointer
(`active.local.md` when present, otherwise `active.example.md`). Then run:

```bash
missis show
missis --ag-brief
```

Use `missis show --context` when the active project or group needs to be
confirmed. Treat the context file and active pointer as handoff metadata only;
the event store remains authoritative for ticket content.

Use the existing ticket workflow from there. Do not create a ticket merely to
confirm that setup succeeded.

## Troubleshooting and safety

- If GitHub or the Go module is unavailable, stop and report the missing
  network access or use the local-checkout alternative.
- If `missis` is not found after installation, check the Go bin directory on
  `PATH` and rerun the version check.
- If `.missis` already exists, preserve it and inspect health; do not create a
  second store beside it unless an operator explicitly chooses a separate
  store workflow.
- If the agent lacks write permission, report the exact target path and stop
  before changing files.
- Never use destructive cleanup to recover from a setup error. Missis uses an
  append-only event ledger and correction is handled through its normal
  retraction workflow.
