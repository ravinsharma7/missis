# Agent-facing benchmark harness (manual only)

`benchmark-agent-brief.sh` compares four cold-start configurations across
adversarial ticket workflows. It measures semantic resistance and performance
instead of treating any created ticket as success:

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

## Skill usage

The skill itself is provider-neutral: [SKILL.md](/home/ravin/Projects/missis/tools/skills/missis/SKILL.md)
only depends on the `missis` CLI and never references Codex. The portable
definition is `tools/skills/missis/SKILL.md` in this repo; copy that directory into
any agent's skills location. For Codex, that location is
`~/.codex/skills/missis`, and the optional `agents/openai.yaml` only supplies
Codex UI metadata.

Install from this checkout with the CLI:

```bash
missis --ag-install-skill --dest "${CODEX_HOME:-$HOME/.codex}/skills/missis"
```

Without `--dest` it defaults to `$CODEX_HOME/skills/missis`; `--force`
overwrites an existing install.

This benchmark tests the Codex harness, so it toggles the skill by moving the
Codex skills directory aside for `baseline`/`pointer` and leaving it present
for `skill`/`brief`, restoring it on exit. `--plan` prints the skill path and
whether it is present. In a session, invoke it with `$missis` or by asking for
missis ticket work.

## Hermeticity and temp dirs

The scratch projects are self-contained under `./temp/run-<timestamp>/` and
never use `/tmp`. Logs, per-config transcripts, provider files, and the
auto-generated `results.md` land in `./temp/run-<timestamp>/logs`; the
`temp/` directory is gitignored.

The scratch projects do NOT inherit this project's full `AGENTS.md`. The
baseline and skill configs get no AGENTS.md at all, and the pointer and brief
configs get a self-contained pointer fixture that only tells the agent to run
`missis --ag-brief`. That keeps the comparison from being biased by AG1-AG7.
Every scenario also injects deliberately stale legacy context and active-pointer
files. They are test data only and must not be used as task instructions.

## Recording results

Every real run writes a portable folder under `./temp/run-<timestamp>/`:
`results.md` (the table), `logs/` (one transcript per scenario/config/iteration),
and before/after store projections. Each row is keyed by
`scenario/config#iteration` and links to its relative log path, so every number
can be traced back to its exact model run.

To publish the best run instead of sharing a machine-local path, pass
`--keep`, which copies `results.md`, `logs/`, and the catalog patch into
`testsuite/benchmarks/results/run-<timestamp>/`. Commit that folder and anyone
can open the same table and raw transcripts without access to this machine.

To record the same table in a missis ticket, use the committed file as the
source of truth:

```bash
missis set '#32/notes' "$(< testsuite/benchmarks/results/<run-id>/results.md)"
```

Provider detection: the harness reads `model` and `model_provider` from
`~/.codex/config.toml` (the codex config reference documents these keys, with
`openai` as the default provider) and classifies the family as `deepseek`,
`openai-gpt`, or `custom`. Override detection with `CODEX_MODEL_PROVIDER`,
`CODEX_MODEL`, or `--provider`.

Compatibility: if the configured `model_catalog_json` advertises features the
installed codex CLI cannot parse (for example `max` or `ultra` reasoning
levels), the harness patches a temp copy of the catalog (`max`/`ultra` ->
`xhigh`) and passes it via `-c model_catalog_json=...`. Failed or zero-turn
Codex sessions are recorded as `blocked`, never as semantic passes. The
scratch agent also gets the freshly built `missis` binary on PATH, so the run
exercises the new CLI rather than a stale installed copy. Override the codex
binary and run flags with `CODEX_BIN` and `CODEX_RUN_ARGS`.

Scenarios currently include:

- `explicit-title`: create a ticket with a title that conflicts with stale
  focus text; the exact title must be created.
- `missing-title`: request a ticket without a title; the safe result is no
  mutation rather than deriving a title from legacy files.
- `target-ref`: update `#1` while a different ticket and stale pointer compete
  for attention; only the requested ref may change.

Metrics per run: semantic pass/fail, wall time, `exec` tool-call count,
assistant turn count, best-effort model token count, transcript bytes, before and
after ticket counts, and exit code. The benchmark therefore selects a winner by
correctness first, then compares performance among configurations that pass.
The baseline uses the
`BASELINE_REF` missis binary with the skill moved aside, so it reproduces the
old discovery path; the other three configs use the working tree. Once the
changes are committed, pass `--baseline-ref <pre-change-commit>` (e.g.
`3031333`) so the baseline stays truly pre-change. The skill is restored at
exit.

Every real run also writes a self-contained `results.md` table into the
printed scratch directory alongside the per-config transcripts, with the
provider, model, catalog, baseline ref, and iteration count captured for
repeatability.

Run `--plan` first to preview the matrix and the provider detection without
spending tokens:

```bash
testsuite/benchmarks/benchmark-agent-brief.sh --plan
```

Then execute (optionally with `--iterations N` or `--scenario NAME`):

```bash
testsuite/benchmarks/benchmark-agent-brief.sh --iterations 2 --baseline-ref 3031333
testsuite/benchmarks/benchmark-agent-brief.sh --scenario target-ref --iterations 3
```
