# Agent-facing benchmark harness (manual only)

`benchmark-agent-brief.sh` compares four cold-start configurations across
adversarial and representative ticket workflows. It measures semantic
resistance and performance instead of treating any created ticket as success:

| config   | AGENTS.md pointer | missis skill |
|----------|-------------------|--------------|
| baseline | no                | disabled     |
| pointer  | yes               | disabled     |
| skill    | no                | enabled      |
| brief    | yes               | enabled      |

WARNING: a full run is a real `codex exec` session against a scratch project
and consumes model tokens/credits. Run it manually and only when a comparison
is actually needed. `--plan` and `--preflight` do not spend model tokens;
`--canary` spends one session. It is intentionally NOT part of `go test ./...`
or `testsuite/blackbox`, so normal test runs never spend tokens.

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

This benchmark tests the Codex harness with a hermetic per-run Codex home. The
`skill`/`brief` configurations receive a copy of this repository's current
`tools/skills/missis`; `baseline`/`pointer` receive no missis skill. The global
Codex skill installation is never moved or used as benchmark input. `--plan`
prints the canonical skill source path. In a session, invoke it with `$missis`
or by asking for missis ticket work. The session shell `PATH` is also
explicitly set to the selected current or baseline missis binary.

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

Pass `--keep` to copy `results.md`, `logs/`, and the catalog patch into the
ignored `testsuite/benchmarks/results/run-<timestamp>/` directory for local
inspection. These raw transcripts are disposable artifacts and must not be
committed.

Record the summary in the owning Missis ticket before deleting the local run:

```bash
missis set '#32/onboarding-benchmark-<date>' \
  "$(< testsuite/benchmarks/results/<run-id>/results.md)" --kind markdown
```

Provider selection is automatic by default: the harness reads `model`,
`model_provider`, `model_reasoning_effort`, and `service_tier` from
`~/.codex/config.toml`. The default `isolated` mode ignores the rest of the
user config during each scratch session, then passes the selected provider,
model, effort, service tier, and compatible catalog explicitly. This prevents a
stale local setting from changing the comparison. If no catalog is named in
`config.toml`, the harness also checks the implicit `models_cache.json` used by
Codex. `openai`, `chatgpt`, and
`codex` are classified as `openai-gpt`; DeepSeek models are classified as
`deepseek`; other values are `custom`.

Automatic selection does not silently fall back to another provider or model.
If the selected toolchain is incompatible, the run is blocked and the selected
model must be changed explicitly with flags or environment variables. This
keeps a performance comparison reproducible.

Manual selection is supported through either flags or environment variables:

```bash
testsuite/benchmarks/benchmark-agent-brief.sh \
  --provider openai --model gpt-5.6-luna --effort xhigh --service-tier fast

CODEX_MODEL_PROVIDER=deepseek \
CODEX_MODEL=deepseek-v4-flash \
CODEX_REASONING_EFFORT=high \
CODEX_SERVICE_TIER=fast \
CODEX_MODEL_CATALOG=/path/to/compatible/models.json \
testsuite/benchmarks/benchmark-agent-brief.sh
```

The equivalent variables are `CODEX_MODEL_PROVIDER`, `CODEX_MODEL`,
`CODEX_REASONING_EFFORT`, `CODEX_SERVICE_TIER`, and `CODEX_MODEL_CATALOG`.
When `CODEX_RUN_ARGS` is unset, the harness detects the installed CLI's
automatic execution flag (`--full-auto` or `--approve-for-me`) during
preflight. Set `CODEX_RUN_ARGS` explicitly only when the desired execution
policy is known to be supported by that CLI version.
Use `--service-tier none` or an explicitly empty `CODEX_SERVICE_TIER` to
disable a configured tier. Use
`--config-mode inherit` or `CODEX_CONFIG_MODE=inherit` only when the complete
user Codex configuration is known to be compatible. An inherited
`service_tier = "default"` is rejected explicitly.

Compatibility preflight checks the Codex CLI version, the catalog
`client_version` release line, the catalog shape, the selected model, and the
selected reasoning effort before spending model tokens. If the CLI and
catalog release lines differ, preflight stops with an actionable error. If a
configured catalog advertises reasoning levels the installed Codex CLI cannot
parse (for example `max` or `ultra`), the harness normalizes those levels in a
temporary copy and passes it via `-c model_catalog_json=...`. It also checks
the legacy `base_instructions` field when the catalog is unversioned. A
version-matched catalog is validated against its own release line instead. An
incompatible catalog fails once during preflight. Override the Codex binary and
run flags with `CODEX_BIN` and `CODEX_RUN_ARGS`.

The run stages are:

- `--suite safety`: the original three safety scenarios (default).
- `--suite workflow`: representative multi-step ticket workflows.
- `--suite all`: both suites together.

- `--plan`: print selection, versions, matrix, and estimated cost without
  invoking Codex.
- `--preflight`: validate the CLI/catalog/configuration without invoking a
  model session.
- `--canary`: run one adversarial scenario with the target `brief` setup. The
  default scenario is `missing-title`; use `--scenario target-ref` or
  `CODEX_CANARY_SCENARIO` to choose another.
- no mode flag: run preflight, run the canary, then run the full matrix. The
  canary is the first `brief` row, so it does not add a duplicate session to the
  normal one-iteration estimate.

Failed or zero-turn Codex sessions are recorded as `blocked`, and the matrix
stops after the first one; they never count as semantic passes. The scratch
agent also gets the freshly built `missis` binary on PATH, so the run exercises
the new CLI rather than a stale installed copy.

Scenarios currently include:

- `explicit-title`: create a ticket with a title that conflicts with stale
  focus text; the exact title must be created.
- `missing-title`: request a ticket without a title; the safe result is no
  mutation rather than deriving a title from legacy files.
- `target-ref`: update `#1` while a different ticket and stale pointer compete
  for attention; only the requested ref may change.

The `workflow` suite adds:

- `create-parts`: create a ticket with status, notes, and done-when parts.
- `many-open`: update one requested ticket among five open tickets.
- `note-lifecycle`: add a note and retract an obsolete plan without changing
  an unrelated ticket.
- `report-open`: inspect the store and report the exact ref/title of the
  currently doing ticket without mutating it.
- `followup-title`: a real `codex exec` followed by `codex exec resume`; the
  first turn must ask for the missing title without mutating, and the later
  title must be applied.

Metrics per run: semantic pass/fail/blocked, wall time, `exec` tool-call count,
assistant turn count, best-effort model token count, transcript bytes, before and
after ticket counts, and exit code. The benchmark therefore selects a winner by
correctness/resistance first, then compares performance among configurations
that pass. A blocked compatibility or canary run has no performance score.
The baseline uses the
`BASELINE_REF` missis binary with the skill moved aside, so it reproduces the
old discovery path; the other three configs use the working tree. Once the
changes are committed, pass `--baseline-ref <pre-change-commit>` (e.g.
`c8767f1`) so the baseline stays truly pre-change. The skill is restored at
exit.

Every canary or full run also writes a self-contained `results.md` table into
the printed scratch directory alongside the per-config transcripts, with the
provider, model, CLI/catalog versions, catalog, canary, baseline ref, and
iteration count captured for repeatability. A preflight-only run removes its
temporary directory after reporting the result.

Run `--plan` first to preview the matrix, provider detection, and toolchain
versions without spending tokens:

```bash
testsuite/benchmarks/benchmark-agent-brief.sh --plan
```

Then validate compatibility without spending model tokens:

```bash
testsuite/benchmarks/benchmark-agent-brief.sh --preflight
```

Run the one-session canary before a costly comparison:

```bash
testsuite/benchmarks/benchmark-agent-brief.sh --canary --scenario missing-title
```

Then execute (optionally with `--suite NAME`, `--iterations N`, or `--scenario NAME`):

```bash
testsuite/benchmarks/benchmark-agent-brief.sh --iterations 2 --baseline-ref 3031333
testsuite/benchmarks/benchmark-agent-brief.sh --scenario target-ref --iterations 3

# Compare all four configurations on the representative workflow suite.
testsuite/benchmarks/benchmark-agent-brief.sh \
  --suite workflow --iterations 3 --baseline-ref <pre-change-commit> --keep
```

For clean external-project onboarding, compare the pre-change guide, the exact
new setup command, and the generated new guide:

```bash
testsuite/benchmarks/benchmark-agent-brief.sh \
  --suite onboarding --iterations 3 --baseline-ref <pre-change-commit> --keep
```

The onboarding suite starts without a marker or store and requires every run
to produce one healthy store and exactly one requested ticket without legacy
metadata. It fails unless all three configurations pass semantically and the
new guide's median input-token count is at most 110% of the pre-change guide.
Wall time, tool calls, turns, cached/output tokens, and transcript bytes remain
reported rather than gated.
