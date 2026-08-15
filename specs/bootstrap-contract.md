# Phase 1 bootstrap contract

This document is temporary. It pins the first runnable contract for the Phase
1 implementation and testsuite. Stabilized rules will be back-ported into
`specs/missues-issue-specification.v2.md`, after which this document and the
bootstrap requirement register will be retired.

## 1. Binary and storage

The public binary is:

```text
missis
```

The only top-level subcommands are:

```text
missis new
missis show
missis set
```

The durable store is a single SQLite database file:

- `MISSIS_STORE` overrides the path;
- otherwise the path is `~/.local/share/missis/missis.db`;
- parent directories are created automatically;
- a clean run must be able to reopen the same file and recover the same tickets.

## 2. Common flags

All commands accept:

```text
--json
--actor <actor-ref>
--effective-at <RFC3339 timestamp>
```

`recorded_at` is always system-assigned. A caller cannot forge transaction
time. If `--effective-at` is omitted, `effective_at` defaults to `recorded_at`.

Machine timestamps use RFC 3339 in UTC.

## 3. Exit codes

```text
0  success
2  invalid command or input
3  reference not found
4  validation or ontology failure
5  optimistic concurrency conflict
8  storage or integrity failure
```

Other exit classes from the specification are not required in Phase 1.

## 4. `missis new`

```text
missis new [title] [--json] [--actor <ref>] [--effective-at <ts>]
            [--project <name>] [--type <name>] [--priority <value>]
            [--tag <value>]
```

Behavior:

- creates one ticket identity;
- creates a root `title` part with the supplied title or empty string;
- creates a root `status` part with value `open`;
- creates a root `priority` part when `--priority` is supplied;
- stores repeated `--type` and `--tag` values as metadata parts;
- returns the new ticket reference.

JSON success:

```json
{
  "ref": "#184",
  "id": "ticket:01J5...",
  "title": "Fix retry race",
  "status": "open",
  "project": "safedesign",
  "recorded_at": "2026-08-15T05:03:21Z"
}
```

`project` is `null` when not supplied.

## 5. `missis show`

```text
missis show [ref] [--json]
            [--at <ts>]
            [--effective-at <ts>] [--known-at <ts>]
            [--history] [--since <ts>] [--between <ts..ts>]
```

Behavior:

- no reference: list current tickets;
- ticket reference such as `#184`: show the ticket projection;
- part reference such as `#184/evidence/race-test`: show that part subtree;
- event reference such as `@e114`: show one exact event;
- temporal flags select a projection;
- `--history` returns ordered events for the selected stream.

JSON ticket projection:

```json
{
  "ref": "#184",
  "id": "ticket:01J5...",
  "title": "Fix retry race",
  "status": "open",
  "recorded_at": "2026-08-15T05:03:21Z",
  "parts": {
    "title": {
      "id": "part:01K2...",
      "path": "title",
      "value": "Fix retry race",
      "value_kind": "text",
      "parent_id": null,
      "created_by": "event:01J5..."
    },
    "status": {
      "id": "part:01K2...",
      "path": "status",
      "value": "open",
      "value_kind": "status",
      "parent_id": null,
      "created_by": "event:01J5..."
    }
  }
}
```

`parts` is a flat map keyed by current path. Canonical `id` remains stable
after rename or move.

JSON history:

```json
{
  "events": [
    {
      "id": "event:01J5...",
      "alias": "@e1",
      "sequence": 1,
      "operation": "set-value",
      "target": "#184/status",
      "value": "open",
      "recorded_at": "2026-08-15T05:03:21Z",
      "effective_at": "2026-08-15T05:03:21Z",
      "actor": "human/ravin"
    }
  ]
}
```

## 6. `missis set`

```text
missis set <ref> [value] [--json]
           [--retract] [--recursive] [--reason <text>]
           [--add] [--name <segment>] [--parent <ref>]
           [--supersedes @eN] [--because <ref>]
           [--if-current @eN]
           [--actor <ref>] [--effective-at <ts>]
           [--idempotency-key <key>]
```

Phase 1 operations:

- set or retract a scalar part value;
- add a value to a list-like part;
- rename a part with `--name`;
- move a part with `--parent`;
- recursive retraction with `--retract --recursive`;
- supersede an exact event with `--supersedes`;
- expected-revision check with `--if-current`.

JSON success:

```json
{
  "ref": "#184/status",
  "event": "@e2",
  "operation": "set-value",
  "value": "doing"
}
```

## 7. JSON errors

```json
{
  "error": "validation_failed",
  "target": "#184/status",
  "message": "cannot transition to done",
  "ontology": null,
  "missing_obligations": []
}
```

`error` is a stable machine code. `target` may be null for parse errors.

## 8. Black-box test harness contract

The portable testsuite:

- reads `MISSIS_BIN` for the compiled `missis` binary;
- sets `MISSIS_STORE` to a fresh temporary SQLite file for each test;
- invokes the binary with `exec.Command`;
- checks stdout/stderr and exit codes;
- does not import implementation packages.

The suite may build the binary in test setup, but the test logic must remain
binary-contract-based.
