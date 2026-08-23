# Local alpha readiness checklist

This checklist is the release evidence map for the current local alpha. It
does not change the authoritative requirements registry or close ticket #93.

## Implementation evidence

| Area | Implementation evidence | Operational boundary |
| --- | --- | --- |
| Three-command workflow | cmd/missis exposes new, show, and set | Maintenance stays in missis-tools |
| Artifact storage | Content-addressed local backend with per-store isolation | MISSIS_ARTIFACT_STORE is the explicit override |
| Mixed content | Markdown, CodeRef, GitRef, image, audio, video, and artifact Parts | URLs and embedded media remain inert |
| Ordering | Core-assigned order keys, move events, rebuildable projections | Dense insertion may rebalance siblings |
| Backup/restore | SQLite snapshot, manifest, artifact sidecar, completion marker | Restore requires a new destination |
| Coordination | Shared client leases and exclusive maintenance leases | Migration and GC are offline operations |

## Automated evidence

| Acceptance surface | Evidence |
| --- | --- |
| Public alpha workflow | testsuite/blackbox/local_alpha_readiness_test.go |
| Mixed-content and Markdown transport | pkg/missis/mixed_content_integration_test.go, pkg/missis/inline_content_integration_test.go |
| Backup and restore integrity | pkg/missis/backup_test.go, internal/application/backup_concurrency_test.go |
| Migration and GC | tools/missis-tools/main_test.go |
| Projection repair interleavings | internal/store/derived_test.go |
| Installation surface | testsuite/scripts/test-install.sh, testsuite/scripts/test-published-install.sh |

Run the acceptance suite from the repository root:

    go test ./...
    go test -race ./...
    go test -bench . ./internal/store
    go run ./tools/check-done
    go run ./tools/coverage --registry specs/requirements-registry.v3.json
    git diff --check

## Operator checks

- Install missis and missis-tools from the same immutable tag or commit.
- Run missis --version, missis --ag-brief, and missis-tools --help.
- Use a fresh project or verify the existing .missis store before mutation.
- Set MISSIS_ARTIFACT_STORE when the default user-data location is not
  appropriate.
- Stop active clients before artifacts migrate or confirmed artifacts gc.
- Keep migration quarantines until rollback is no longer required.
- Run backup verify and require state=complete before restore or transfer.
- Restore only into a new destination and verify the restored artifact root.

## Deferred requirements

The following remain intentionally incomplete and are tracked separately:

- #101 — S3-compatible and Harbor artifact backends.
- #102 — External plugin loading, capability enforcement, and isolation.
- #103 — Asynchronous ingestion jobs, retries, and worker processing.
- #104 — Resumable artifact uploads.

Search, richer renderer execution, generic backup-provider adapters, and
ordered inline composition inside one Markdown value are also outside this
local alpha. No acceptance result should mark ticket #93 done while those
follow-ups remain.
