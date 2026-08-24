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

<!-- BEGIN GENERATED MISSIS ONBOARDING -->
## Install and set up a project

Missis requires Go 1.26 or newer. The reproducible clean-project path installs
the verified binary pair from one immutable stable tag and sets up the current
directory:

~~~bash
go run github.com/ravinsharma7/missis/tools/paired-install@<stable-tag> \
  --project . --json
~~~

When the verified pair is already installed:

~~~bash
missis --setup --project . --json
missis --ag-brief
~~~

See the [standalone setup guide](docs/agent-setup.md) for PowerShell, result
states, an immutable URL handoff, and migration from removed setup flags. Use
the [legacy migration guide](docs/missis-migration-prompt.md) only for old
`.missis.d` metadata and [storage compatibility](docs/storage-compatibility.md)
for backup, restore, artifacts, and release compatibility.
<!-- END GENERATED MISSIS ONBOARDING -->

## Go SDK

The public facade is:

```go
import "github.com/ravinsharma7/missis/pkg/missis"
```

It provides store discovery, a `Client` wrapper, event helpers, health,
backup, and verification methods. `cmd/missis` is intended to stay a thin CLI
layer.

## Reference documentation

- [Canonical specification](specs/missues-issue-specification.v2.md)
- [Phase 1 requirements](specs/phase1-requirements.md)
- [Storage and release compatibility](docs/storage-compatibility.md)
- [Guarantees and performance](specs/missues-issue-specification.v2.md#31-guarantees-and-performance)
- [Ticket TUI cheatsheet](docs/ticket-tui-cheatsheet.md)
