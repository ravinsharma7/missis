# Phase 1 traceability check

This check replaces the earlier prose-only implementation gate.

It is deterministic: it runs the Go tests, builds the `missis` binary, runs the
portable black-box suite, and then verifies that every required requirement ID
has an explicit reference in a test file.

## Required IDs

The checker extracts required IDs from two places:

- every `PH1-*` row in `specs/phase1-requirements.md`;
- every `N*` row in `specs/phase1-should-backlog.md` whose decision is
  `adopt`. Older projects may temporarily use the legacy
  `.missis.d/phase1-should-backlog.md` path; the checker reports that fallback
  so it can be migrated.

Deferred and rejected SHOULD items are not required.

## Coverage convention

Test files declare coverage with a literal comment:

```go
// covers PH1-CLI-001
// covers N002
```

The checker greps test files for those exact IDs. The ID may appear in a test
name, function comment, or nearby comment; the important rule is that it is
present in the test source.

## Exit behavior

```text
go test ./...                     must pass
black-box suite against built bin must pass
every required ID                 must appear in a test file
```

Any missing link fails the check.
