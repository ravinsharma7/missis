# Agent-facing benchmark harness (manual only)

`benchmark-agent-brief.sh` compares four cold-start configurations for the
prompt "create a missis ticket":

| config   | AGENTS.md pointer | missis skill |
|----------|-------------------|--------------|
| baseline | no                | disabled     |
| pointer  | yes               | disabled     |
| skill    | no                | enabled      |
| brief    | yes               | enabled      |

WARNING: each run is a real `codex exec` session against a scratch project and
consumes model tokens/credits. Run it manually and only when a comparison is
actually needed. It is intentionally NOT part of `go test ./...` or
`testsuite/blackbox`, so normal test runs never spend tokens.

Provider detection: the harness reads `model` and `model_provider` from
`~/.codex/config.toml` (the codex config reference documents these keys, with
`openai` as the default provider) and classifies the family as `deepseek`,
`openai-gpt`, or `custom`. Override detection with `CODEX_MODEL_PROVIDER`,
`CODEX_MODEL`, or `--provider`.

Compatibility: if the configured `model_catalog_json` advertises features the
installed codex CLI cannot parse (e.g. a deepseek catalog with a `max` reasoning
effort on an older CLI), the harness patches a temp copy of the catalog
(`max` -> `xhigh`) and passes it via `-c model_catalog_json=...`. The scratch
agent also gets the freshly built `missis` binary on PATH, so the run exercises
the new CLI rather than a stale installed copy. Override the codex binary and
run flags with `CODEX_BIN` and `CODEX_RUN_ARGS`.

Metrics per run: wall time, `exec` tool-call count and assistant turn count
from the codex transcript, tickets created in the scratch store, exit code,
and whether the run proceeded or blocked. The baseline uses HEAD's AGENTS.md
and HEAD's missis binary (pre-change) with the skill moved aside, so it
reproduces the old discovery path; the other three configs use the working
tree. The skill is restored at exit.

Run `--plan` first to preview the matrix and the provider detection without
spending tokens:

```bash
testsuite/benchmarks/benchmark-agent-brief.sh --plan
```

Then execute (optionally with `--iterations N` for repeated runs):

```bash
testsuite/benchmarks/benchmark-agent-brief.sh --iterations 2
```
