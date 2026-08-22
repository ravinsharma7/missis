# Specification authority

This directory contains both active contracts and retained design history.
Agents and implementers must use the active documents first.

## Active contracts

- `missues-issue-specification.v2.md` — canonical product and model contract.
- `phase1-requirements.md` — current Phase 1 requirements.
- `requirements-registry.v3.json` — requirement identifiers and traceability.
- The repository's `.missis-store/` — authoritative current ticket data.

The canonical agent workflow is the output of:

```bash
missis --ag-brief
```

Use [`docs/onboarding-workflows.md`](../docs/onboarding-workflows.md) for the
human decision guide covering setup, ordinary agent work, reviewed project
handoffs, and migration. It is navigation, not an additional contract.

## Traceability metadata

- `phase1-should-backlog.md` — decisions for Phase 1 SHOULD requirements used
  by the traceability checker; it is not agent task direction or a product
  contract.

## Retained, non-authoritative material

- `missues-issue-specification.z.v1.md` — historical specification, retained
  for comparison and migration only.
- `bootstrap-contract.md` — temporary Phase 1 implementation contract; it is
  not a general agent bootstrap guide.
- `schema-declaration.subspec.md` — working ontology draft tracked by open
  ticket `#27`; retirement and the final immutable Git reference are tracked
  by `#100`. It is not normative until merged into v2 or explicitly rejected.
  Do not use it as task direction or agent bootstrap guidance.
- `change.md` — specification history and provenance notes.

When a retained draft is merged or rejected, update the owning ticket and
remove the draft in the same change. Do not silently turn a draft into an
active contract by editing it alone.
