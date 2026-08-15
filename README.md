# Three core parts to "missis"
- the core specs
- the core test suite
- the core reference implementation

# Introduction
- missis has three domain commands and a small allowlist of global operational flags. it is purposely simple in the interface, but the implementation is aggressively complicated. This allows any system integration without complicated interfaces.
- The global flag allowlist may grow only for process-level maintenance or inspection flags. It must not introduce new command paths, ticket mutations, or domain workflows; those stay under `new`, `show`, or `set`.
- You can hack and build your own "missis" implementation by reusing and mixing any parts of these. it is organized in this way to make it easy to cleanroom port into another language, or extend or change parts of it for different purposes. These three are always stable and constantly will be made more rigorous so anything project extending from this will always have a strong baseline.
- All three implementation will expose some flaws in each other and can be used as basis to continuously improve each other.
- This project uses AI heavily to experiment with matters.

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

For an interactive shell over those views, use the development tool:

```bash
bash tools/ticket-explorer.sh
```

That tool is not a fourth top-level `missis` command. It wraps `show` and can
list tickets, inspect a ticket or part, emit JSON, follow references, walk
lineage, search, and write Markdown to a file when requested.

For a visual terminal UI, use:

```bash
go run ./tools/ticket-tui
```

The TUI is also a development tool, not a public `missis` command. It can list
tickets, open a ticket, export Markdown, and compare two tickets.
