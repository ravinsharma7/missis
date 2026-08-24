# Specification authority

This directory contains the active contracts and one explicitly protected
working draft. Agents and implementers must use the active documents first.

## Active contracts

- `missues-issue-specification.v2.md` — canonical product and model contract.
- `phase1-requirements.md` — current Phase 1 requirements.
- `requirements-registry.v3.json` — requirement identifiers and traceability.
- The repository's `.missis-store/` — authoritative current ticket data.

The canonical agent workflow is the output of:

```bash
missis --ag-brief
```

Use [`docs/agent-setup.md`](../docs/agent-setup.md) for clean setup and the
handoff to ordinary agent work. It is navigation, not an additional contract.

Phase 1 SHOULD decisions and their rationale are stored with the corresponding
norms in `requirements-registry.v3.json`; there is no separate backlog to
synchronize.

## Protected, non-authoritative draft

- `schema-declaration.subspec.md` — working ontology draft tracked by open
  ticket `#27`; retirement and the final immutable Git reference are tracked
  by `#100`. It is not normative until merged into v2 or explicitly rejected.
  Do not use it as task direction or agent bootstrap guidance.

When a retained draft is merged or rejected, update the owning ticket and
remove the draft in the same change. Do not silently turn a draft into an
active contract by editing it alone.
