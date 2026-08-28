# Specification authority

This directory contains the active contracts and one explicitly protected
working draft. Agents and implementers must use the active documents first.

## Active contracts

- `missues-issue-specification.v2.md` — frozen canonical v2 product and model
  contract. Only correctness/security/data-loss errata enter v2.
- `event-store-v3-alpha.md` — read-only distribution snapshot of the
  consumer-neutral event-store alpha contract selected by
  `event-tooling.lock.json`.
- `cross-store-references-v3-alpha.md` — read-only distribution snapshot of
  the subordinate external-reference contract selected by the same lock.
- `phase1-requirements.md` — current Phase 1 requirements.
- `requirements-registry.v3.json` — requirement identifiers and traceability.
- The existing project store selected through reviewed Missis discovery —
  authoritative runtime ticket data for that project. The standalone source
  mirror itself intentionally contains no development ticket store.

The canonical agent workflow is the output of:

```bash
missis --ag-brief
```

Use [`docs/agent-setup.md`](../docs/agent-setup.md) for clean setup and the
handoff to ordinary agent work. It is navigation, not an additional contract.

Phase 1 SHOULD decisions and their rationale are stored with the corresponding
norms in `requirements-registry.v3.json`; there is no separate backlog to
synchronize.

## Neutral contract authority

Writable product and neutral protocol authority moved to the private
`git@github.com:ravinsharma7/skunkwork.git` integration repository under
ticket `#121`. Canonical Missis source is `products/missis` there. The
standalone repository is a one-way build/release mirror and consumes one
explicit contract authority commit through `event-tooling.lock.json`. The
local Markdown remains present so the requirements registry, documentation,
source tests, and released format-6 implementation are self-contained.

Do not synchronize in both directions. A reviewed skunkwork contract update
produces a new lock and read-only snapshot here. These snapshots may be
removed only when every local source reference has been replaced by a pinned,
offline-verifiable contract bundle and the release/conformance checks pass.

## Protected, non-authoritative draft

- `schema-declaration.subspec.md` — working ontology draft tracked by open
  ticket `#27`; retirement and the final immutable Git reference are tracked
  by `#100`. It is not normative until merged into v2 or explicitly rejected.
  Do not use it as task direction or agent bootstrap guidance.

When a retained draft is merged or rejected, update the owning ticket and
remove the draft in the same change. Do not silently turn a draft into an
active contract by editing it alone.
