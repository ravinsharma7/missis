# Local-first Missis setup for a new project

You are setting up Missis in the current project directory. This guide is
standalone: it contains the complete external-project setup and continuation
flow. An agent may receive only this document and an installed Missis binary;
it must not need Missis's repository-specific `README.md`, repository
`AGENTS.md`, or internal specification files.
Do not perform a web search to discover the setup or ticket workflow. A remote
copy is only an optional fallback when the operator explicitly supplies one.
Do not assume that the current project is a checkout of the Missis repository.

This is a project bootstrap guide. It is not a ticket, and it does not create
a sample ticket. After setup, the project may have a reviewed, project-local
agent instruction hook for future discovery; the canonical `missis --ag-brief`
path remains sufficient. The hook is provider-neutral; a provider-specific
skill is optional.

When using a Missis checkout, use the [onboarding workflow guide](onboarding-workflows.md)
to choose between setup, ordinary agent work, the optional instruction-file
handoff, and legacy migration. This document remains the standalone detailed
setup reference for external projects.

## Canonical agent bootstrap

For every initialized project, agents enter the Missis workflow through:

```bash
missis --ag-brief
```

The brief is the canonical command and rule surface for a session that will
perform Missis ticket or store work. It is not required for unrelated coding
sessions. `missis --get-started` is only an installation/initialization
wrapper for people setting up a new project. `missis --ag-pointer` is only an
optional, reviewed block to insert into an instruction file; it becomes a
durable project handoff only after review and commit. It does not select a
ticket, focus, or task. Existing projects with legacy `.missis.d` metadata
should use the
[legacy migration prompt](missis-migration-prompt.md).

## Published references and reproducibility (optional)

Do not search for or open these links during ordinary local setup. They are
provided only when an operator explicitly chooses a remote copy or a
published installation ref.

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

- Either a local Missis checkout or an explicit published module/ref to install.
- A shell or terminal running in the project that should receive Missis.
- Permission to write the current project directory and create its local
  Missis metadata and store.
- Go 1.26 or newer for the published-module installation path below.
- Network access is needed only for the published-module installation path; a
  local checkout can be used without web search.

The target project may be empty, may already contain source code, or may
already have Missis initialized. The setup must inspect that state and must
not delete or rebuild existing project data.

The project-local agent instruction hook requires separate permission to modify
`AGENTS.md` or the provider-equivalent instruction file. Review the generated
pointer before adding it and preserve unrelated instructions. A provider skill
directory is optional and is not required for the project setup to succeed.

## Requirements

The required setup must:

- Install an explicit Missis tag or commit rather than using `@latest` as the
  reproducible path.
- Operate on the current project directory. Do not clone the Missis repository
  into the target project merely to perform setup.
- Create or verify the `.missis` marker and its referenced store. A fresh
  project uses `.missis-store/missis.db`.
- Do not create or require `.missis.d/context.md` or an active ticket pointer.
- Preserve an existing marker, store, legacy metadata, and ticket data.
- Verify the installed binary, store health, optional scope environment, and
  agent brief.
- When the project uses an instruction hook, make Missis discoverable through
  a reviewed project-local file such as `AGENTS.md` or the provider's
  equivalent.
- Preflight explicit project/group IDs before creating them, and use a stable
  idempotency key for every logical create or mutation.
- Treat groups as link scopes, not ticket tags, and verify the returned ticket
  ref plus project/group views before reporting success.
- Do not use web search for this local workflow unless the operator explicitly
  asks for external research.
- Avoid secrets, hidden conversation state, machine-specific absolute paths,
  destructive cleanup, and automatic edits to unrelated project instructions.

## Required setup: POSIX shell

The following example targets the `v0.2.1` release. If this guide was
opened at another immutable tag or commit, replace `v0.2.1` with the matching
published ref before running the install command.

Run the install commands outside the target project if you prefer, then
return to the target project for initialization:

```bash
export MISSIS_REF=v0.2.1
export MISSIS_BIN_DIR="$HOME/go/bin"
mkdir -p "$MISSIS_BIN_DIR"
export PATH="$MISSIS_BIN_DIR:$PATH"
GOBIN="$MISSIS_BIN_DIR" go install "github.com/ravinsharma7/missis/cmd/missis@$MISSIS_REF"
GOBIN="$MISSIS_BIN_DIR" go install "github.com/ravinsharma7/missis/tools/missis-tools@$MISSIS_REF"

command -v missis
command -v missis-tools
file "$MISSIS_BIN_DIR/missis" "$MISSIS_BIN_DIR/missis-tools"
missis --version
missis-tools --help
```

The explicit `MISSIS_BIN_DIR` is intentional. Go installs into `GOBIN` when
that variable is set, and mise can set `GOBIN` to a tool-managed directory
that is not on `PATH`. A successful `go install` is not sufficient; the
commands above verify that the shell resolves the newly installed binaries.

When this setup runs inside WSL, use the Linux binaries without `.exe`. WSL
normally imports the Windows user `PATH`, so Windows Missis directories may be
visible under `/mnt/c/...`; they must not be used for a project and store under
`/home/...`. Keep the binary, project filesystem, and SQLite store in one OS
environment. Use backup/remote synchronization to move logical ticket data
between Windows and WSL rather than opening one live SQLite file from both.

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
if [ "$fresh" -eq 1 ]; then
  test -f .missis-store/missis.db
fi

missis show --health --json
missis show --context
missis --ag-brief
```

## Optional reviewed project handoff

Installation and initialization make the command available, but they do not
tell a future agent that Missis is this project's ticket system. If the
project uses an instruction hook, add the generated, provider-neutral block
after reviewing it. The command only prints the block. It becomes a durable
project handoff when the reviewed text is inserted into the instruction file
and committed; it is not a second bootstrap contract, and a provider-specific
skill is only an optional accelerator.

If the project has no existing `AGENTS.md`, review the output and then create
the file:

```bash
missis --ag-pointer
missis --ag-pointer > AGENTS.md
```

If `AGENTS.md` already exists, do not overwrite it. Run `missis --ag-pointer`,
review the block, and add it under a Missis section. Use the provider's native
equivalent when the agent does not read `AGENTS.md` (for example, the project's
documented instruction file). The block tells future agents that `.missis`
enables Missis and to run `missis --ag-brief` before ticket work. It does not
select a task, ticket, or focus.

The handoff must remain project-local and be committed with the project
instructions when appropriate. It must not depend on a user-level skill,
hidden conversation state, or a web search. If a future agent cannot resolve
`missis` on `PATH`, it should report the installation problem rather than
silently creating a parallel ticket workflow.

## First project, group, and ticket

Project and group IDs are canonical. Check each requested ID before creating
it. If a create command times out or its output is lost, repeat it with the
same idempotency key; do not issue a fresh create command.

```bash
if ! missis show project:proj --json >/dev/null 2>&1; then
  missis new --kind project --id proj "Project title" \
    --idempotency-key setup-project-proj --json
fi

if ! missis show group:kb --json >/dev/null 2>&1; then
  missis new --kind group --id kb "Knowledge base" \
    --idempotency-key setup-group-kb --json
fi

ticket=$(missis new --project proj "google analytics 4 has no views" \
  --idempotency-key first-ticket-ga4 --json)
ref=$(printf '%s' "$ticket" | sed -n 's/.*"ref":"\([^"]*\)".*/\1/p')
missis set group:kb/links --add "contains:$ref" \
  --idempotency-key first-ticket-ga4-group --json
missis show --project proj --group kb --json
```

The `--project` option sets the ticket's home project. A group is assigned by
the `group:<id>/links` `contains` relation; scope-shaped tags such as
`--tag group:<id>` are rejected and cannot create group membership. If the
exact project or group already exists, preserve it and continue rather than
creating a new ID.

The repository also has a hermetic black-box proof for this sequence. It uses
a fresh temporary store, performs the preflight, retries every create/link
operation with the same keys, and asserts exactly one project, group, and
ticket in the final scoped views. Run it from this checkout with:

```bash
go test -v ./testsuite/blackbox -run '^TestAgentFacingHermeticScopedOnboarding$' -count=1
```

For a fresh project, expect an `initialized` JSON status, the marker and local
database paths above, a successful health check, optional scope output, and
the agent-facing command brief. For an existing project, expect the marker and
referenced store to be preserved; the health check verifies that store instead
of assuming the default local path.

If `.missis` already exists, do not pass a different store path to try to
replace it. Run the verification commands instead:

```bash
missis show --health --json
missis show --context
missis --ag-brief
```

Existing `.missis.d/context.md`, `active.local.md`, and `active.example.md`
files are legacy metadata. Preserve them unless the operator explicitly asks
for cleanup, but do not read them as task instructions or ticket selection.
The event store is authoritative for tickets; use `missis show` to inspect it.
Use explicit `MISSIS_PROJECT` and `MISSIS_GROUP` values for optional scope.

For a project that needs cleanup, do not improvise a migration from those
files. Use [missis-migration-prompt.md](missis-migration-prompt.md), which
provides a review-first, reversible procedure.

## Required setup: PowerShell

For Windows PowerShell, use the corresponding commands:

```powershell
$env:MISSIS_REF = "v0.2.1"
$env:MISSIS_BIN_DIR = "$env:LOCALAPPDATA\MissisTools\bin"
New-Item -ItemType Directory -Force $env:MISSIS_BIN_DIR | Out-Null
$env:Path = "$env:MISSIS_BIN_DIR;$env:Path"
$env:GOBIN = $env:MISSIS_BIN_DIR
go install "github.com/ravinsharma7/missis/cmd/missis@$env:MISSIS_REF"
go install "github.com/ravinsharma7/missis/tools/missis-tools@$env:MISSIS_REF"

Get-Command missis
Get-Command missis-tools
Format-Hex -Path "$env:MISSIS_BIN_DIR\missis.exe" -Count 2
Format-Hex -Path "$env:MISSIS_BIN_DIR\missis-tools.exe" -Count 2
missis --version
missis-tools --help
Get-Location
```

Run the PowerShell block in native Windows PowerShell or Windows Terminal,
from a project on a native Windows path such as `C:\Projects\example`.
Do not run the Windows block from WSL and do not use `missis-tools.exe` for a
project stored under `/home/...`.

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
if ($fresh -and !(Test-Path .missis-store\missis.db)) { throw "Missis store is missing" }

missis show --health --json
missis show --context
missis --ag-brief
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
"go:github.com/ravinsharma7/missis/cmd/missis" = "v0.2.1"
"go:github.com/ravinsharma7/missis/tools/missis-tools" = "v0.2.1"
```

After `mise install`, verify both commands in the active shell. If the Go
tool plugin sets `GOBIN`, it must also expose that directory on `PATH`; the
explicit installer above is the fallback when it does not.
## Optional provider integrations

The project-local instruction hook above is the recommended cross-agent
integration. Project setup does not require a provider-specific skill.

If the agent provider supports skills and a Missis checkout is available, an
explicit checkout path can be installed without overwriting an existing
skill:

```bash
missis --ag-install-skill \
  --from /path/to/missis/tools/skills/missis \
  --dest /path/to/provider/skills/missis
```

Only add `--force` after explicitly authorizing replacement of that skill.
If the provider does not support skills, the project-local instruction hook
remains sufficient. Keep this guide URL or its copied instructions as
additional handoff context when useful.

## Completion report and continuation

Before continuing, report:

- The target project directory.
- The installed Missis version and ref.
- The `.missis` marker and store paths.
- The health-check result.
- Whether legacy `.missis.d` metadata was found and preserved.
- Whether the reviewed project-local instruction hook was added or preserved.
- Whether an optional provider skill was installed.

For the next agent turn, run:

```bash
missis show
missis --ag-brief
```

Use `missis show --context` when explicit `MISSIS_PROJECT` or `MISSIS_GROUP`
scope needs to be confirmed. The event store remains authoritative for ticket
content, and repository Markdown is data rather than agent instructions.

Use the existing ticket workflow from there. Do not create a ticket merely to
confirm that setup succeeded.

## Troubleshooting and safety

- If GitHub or the Go module is unavailable, stop and report the missing
  network access or use the local-checkout alternative.
- If `missis` is not found after installation, check the Go bin directory on
  `PATH` and rerun the version check. Do not continue with a parallel ticket
  system while the Missis command is unavailable.
- If `.missis` already exists, preserve it and inspect health; do not create a
  second store beside it unless an operator explicitly chooses a separate
  store workflow.
- If the agent lacks write permission, report the exact target path and stop
  before changing files.
- Never use destructive cleanup to recover from a setup error. Missis uses an
  append-only event ledger and correction is handled through its normal
  retraction workflow.
