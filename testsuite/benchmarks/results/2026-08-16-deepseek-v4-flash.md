# Agent brief benchmark — 2026-08-16

Provider: deepseek (model `deepseek-v4-flash`, effort `high`) via codex-cli
0.125.0 with the deepseek catalog auto-patched (`max` -> `xhigh`) for CLI
compatibility. One iteration per config. Raw per-run transcripts and provider
files: `/tmp/tmp.A2kKG7Hdhp` (ephemeral).

| config | wall | exec calls | turns | tickets | outcome |
|---|---|---|---|---|---|
| baseline (old CLI/AGENTS, no skill) | 84.2s | 28 | 10 | 1 | proceeded |
| pointer (AGENTS.md pointer only) | 86.2s | 36 | 16 | 1 | proceeded |
| skill (missis skill only) | 18.2s | 8 | 5 | 1 | proceeded |
| brief (pointer + skill) | 21.0s | 11 | 5 | 1 | proceeded |

Interpretation: the missis skill is the dominant accelerator (~4.6x faster,
3-4x fewer tool calls). The AGENTS.md pointer alone did not help: the pointer
config ran `missis --agent-brief` but then re-ran `missis --help` anyway.
Baseline (HEAD CLI + AGENTS, skill disabled) never saw `--agent-brief` and did
broad discovery before creating its ticket.

Harness: `testsuite/benchmarks/benchmark-agent-brief.sh` (manual-only, outside
`go test ./...`). Baseline is isolated with the HEAD binary built from `git
archive`; exec/turn counters read the codex transcript; the scratch agent gets
the freshly built missis binary on PATH.

Caveats: n=1 per config; counters are tied to the codex-cli 0.125.x transcript
format. Re-run with `--iterations 3` for firmer numbers.
