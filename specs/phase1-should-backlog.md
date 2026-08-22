# Phase 1 SHOULD backlog

> Historical planning metadata only. This file is not an active contract,
> bootstrap source, or task-direction pointer. The v2 specification, Phase 1
> requirements, and the live Missis store are authoritative.

This file makes the Phase 1 `SHOULD` items actionable. Every row must have a
decision before the Phase 1 traceability check is run.

Decisions:

- `adopt` — implement and test in Phase 1.
- `defer` — move to a later phase with a reason.
- `reject` — explicitly not adopted, with a reason.

| Norm ID | Decision | Reason | Target |
| --- | --- | --- | --- |
| N002 | adopt | `--json`, `--actor`, and `--effective-at` are required for a useful Phase 1 CLI. | CLI and black-box tests. |
| N004 | adopt | Restrict status to `open`, `doing`, `blocked`, `done`. | Validation and CLI tests. |
| N005 | adopt | Require a reason when setting `blocked`. | Validation and CLI tests. |
| N006 | adopt | Reject statuses outside the four-value set. | Validation tests. |
| N009 | adopt | Accept arbitrary parts and recognize conventional names. | Model projection tests. |
| N012 | adopt | Enforce one current structural parent. | Model and black-box tests. |
| N014 | adopt | Do not assign status to subparts. | Model behavior. |
| N015 | adopt | Store a basic `value_kind` for text, scalar, and status values. | Store and JSON tests. |
| N019 | adopt | Recursive retraction is one atomic batch. | Store and black-box tests. |
| N022 | adopt | Generate unique IDs from a random source. | Model/store tests. |
| N024 | adopt | Resolve canonical part IDs without a current path. | Model tests. |
| N028 | adopt | Stale paths return a structured error rather than retargeting. | Model/CLI tests. |
| N029 | adopt | Store canonical IDs in events and provenance fields. | Store tests. |
| N042 | adopt | Support `--effective-at` and `--known-at`. | CLI and temporal tests. |
| N044 | defer | Local timezone rendering is not needed for the first kernel. | Phase 1 follow-up or later. |
| N047 | adopt | Use per-stream sequence numbers. | Store tests. |
| N049 | adopt | Use retraction events for removals. | Store/model tests. |
| N051 | adopt | Superseding events identify the replaced event and reason. | Store/model tests. |
| N052 | defer | Semantic temporal diff can follow a working history view. | Later `show` work. |
| N053 | adopt | Recursive and multi-event operations share a batch ID. | Store tests. |
| N055 | adopt | Store actor, effective time, batch, supersedes, and reason fields now. | Store/model tests. |
| N057 | adopt | Accept stable actor refs such as `human/ravin` or `agent/codex/run-1`. | CLI tests. |
| N058 | defer | External effect capture needs the processor/effects layer. | Phase 6 or later. |
| N106 | adopt | Validate CLI input and references before writing. | CLI/model tests. |
| N107 | adopt | Support `--if-current` for scalar updates. | Concurrency tests. |
| N108 | adopt | Support hierarchy revision checks for move/rename. | Concurrency tests. |
| N109 | adopt | Subtree move and recursive retraction commit atomically. | Concurrency tests. |
| N110 | adopt | Treat independent `--add` operations as commutative. | Concurrency tests. |
| N111 | adopt | One user operation produces one atomic batch. | Store tests. |
| N112 | adopt | Support `--idempotency-key` on `new` and `set`. | Idempotency tests. |
| N113 | adopt | Use structured JSON errors. | CLI and black-box tests. |
