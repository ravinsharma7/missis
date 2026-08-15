#!/usr/bin/env bash
# Four-way cold-start comparison for the prompt "create a missis ticket":
#
#   baseline  no AGENTS.md pointer, missis skill disabled (pre-change control)
#   pointer   AGENTS.md --ag-brief pointer only
#   skill     missis skill only
#   brief     pointer + skill (target state)
#
# Manual-only harness: every run is a real `codex exec` session against a
# scratch project and consumes model tokens/credits. It is intentionally
# outside the Go test suite so `go test ./...` never triggers it. All scratch
# files live under ./temp, never /tmp.
#
# Usage:
#   testsuite/benchmarks/benchmark-agent-brief.sh [--iterations N] [--plan] [--provider P] [--baseline-ref REF] [--keep]
#
# Env:
#   CODEX_BIN             codex binary to run (default: codex from PATH)
#   CODEX_RUN_ARGS        flags for non-interactive run (default: --full-auto)
#   CODEX_EXTRA_ARGS      extra flags for `codex exec` (e.g. --model deepseek-v4-pro)
#   CODEX_MODEL           override the detected model label
#   CODEX_MODEL_PROVIDER  override the detected provider label
#   BASELINE_REF          git ref used for the pre-change baseline (default: HEAD)
#
# Requires: codex CLI on PATH, go, jq. Budget 1-3 minutes per iteration.

set -euo pipefail

ITERATIONS=1
PLAN_ONLY=0
PROVIDER_OVERRIDE=""
BASELINE_REF="${BASELINE_REF:-HEAD}"
KEEP=0
while [ $# -gt 0 ]; do
	case "$1" in
	--iterations)
		ITERATIONS="$2"
		shift 2
		;;
	--plan)
		PLAN_ONLY=1
		shift
		;;
	--provider)
		PROVIDER_OVERRIDE="$2"
		shift 2
		;;
	--baseline-ref)
		BASELINE_REF="$2"
		shift 2
		;;
	--keep)
		KEEP=1
		shift
		;;
	-h | --help)
		sed -n '2,28p' "${BASH_SOURCE[0]}"
		exit 0
		;;
	*)
		echo "unknown argument: $1" >&2
		exit 2
		;;
	esac
done

REPO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CODEX_HOME_DIR="${CODEX_HOME:-$HOME/.codex}"
CODEX_CONFIG_FILE="$CODEX_HOME_DIR/config.toml"
SKILL_DIR="$CODEX_HOME_DIR/skills/missis"
SKILL_HIDDEN="$CODEX_HOME_DIR/skills/.missis.benchmark-hidden"
TEMP_ROOT="$REPO_DIR/temp"
RUN_DIR="$TEMP_ROOT/run-$(date -u +%Y%m%dT%H%M%SZ)"
LOG_DIR="$RUN_DIR/logs"
BIN_DIR="$RUN_DIR/bin"
BIN_HEAD_DIR="$RUN_DIR/bin-head"
BIN="$BIN_DIR/missis"
BIN_HEAD="$BIN_HEAD_DIR/missis"
CODEX_BIN="${CODEX_BIN:-codex}"
CODEX_RUN_ARGS="${CODEX_RUN_ARGS:---full-auto}"
CATALOG_PATCHED=""
CODEX_CATALOG_ARG=""

catalog_path() {
	local catpath=""
	if [ -f "$CODEX_CONFIG_FILE" ]; then
		catpath="$(sed -n 's/^model_catalog_json[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$CODEX_CONFIG_FILE" | tail -1)"
	fi
	if [ -n "$catpath" ]; then
		# config.toml uses ~ for the home directory, not CODEX_HOME
		catpath="${catpath/#~/$HOME}"
		if [ -f "$catpath" ]; then
			printf '%s\n' "$catpath"
		fi
	fi
}

prepare_catalog() {
	local catpath
	catpath="$(catalog_path)"
	if [ -n "$catpath" ]; then
		# Newer catalogs (e.g. deepseek) advertise a "max" reasoning effort that
		# older codex CLIs cannot parse. Patch a temp copy so the run works.
		CATALOG_PATCHED="$RUN_DIR/models-patched.json"
		sed 's/"effort": "max"/"effort": "xhigh"/g' "$catpath" >"$CATALOG_PATCHED"
		CODEX_CATALOG_ARG="-c model_catalog_json=$CATALOG_PATCHED"
	fi
}

# The codex config reference (learn.chatgpt.com/docs/config-file/config-reference)
# documents `model` and `model_provider`; the provider defaults to `openai`.
detect_provider() {
	local provider="${CODEX_MODEL_PROVIDER:-}"
	local model="${CODEX_MODEL:-}"
	local effort=""
	if [ -f "$CODEX_CONFIG_FILE" ]; then
		if [ -z "$provider" ]; then
			provider="$(sed -n 's/^model_provider[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$CODEX_CONFIG_FILE" | tail -1)"
		fi
		if [ -z "$model" ]; then
			model="$(sed -n 's/^model[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$CODEX_CONFIG_FILE" | tail -1)"
		fi
		effort="$(sed -n 's/^model_reasoning_effort[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' "$CODEX_CONFIG_FILE" | tail -1)"
	fi
	provider="${provider:-openai}"
	model="${model:-unknown}"
	local family="custom"
	case "$provider" in
	openai | chatgpt) family="openai-gpt" ;;
	*deepseek* | *DeepSeek*) family="deepseek" ;;
	*) family="custom" ;;
	esac
	if [ "$family" = "custom" ] && printf '%s' "$model" | grep -qi 'deepseek'; then
		family="deepseek"
	fi
	printf '%s %s %s %s\n' "$provider" "$model" "$family" "$effort"
}

IFS=' ' read -r PROVIDER_LABEL MODEL_LABEL FAMILY EFFORT_LABEL <<<"$(detect_provider)"
if [ -n "$PROVIDER_OVERRIDE" ]; then
	FAMILY="$PROVIDER_OVERRIDE"
fi

MATRIX=(
	"baseline|0|0"
	"pointer|1|0"
	"skill|0|1"
	"brief|1|1"
)
RESULT_ROWS=""
POINTER_AGENTS_MD='## missis quick reference

Run `missis --ag-brief` before ticket work. It prints the exact command
surface and rules from the CLI itself.'

if [ "$PLAN_ONLY" = 1 ]; then
	echo "provider: $PROVIDER_LABEL  model: $MODEL_LABEL  family: $FAMILY  effort: $EFFORT_LABEL"
	echo "source: $CODEX_CONFIG_FILE"
	echo "catalog: $(catalog_path)"
	echo "baseline ref: $BASELINE_REF"
	echo "temp root: $TEMP_ROOT"
	if [ -d "$SKILL_DIR" ]; then
		echo "skill: $SKILL_DIR (present)"
	else
		echo "skill: $SKILL_DIR (absent)"
	fi
	echo
	echo "config matrix (no codex sessions will run):"
	printf '%-10s %-10s %-8s\n' "config" "pointer" "skill"
	for row in "${MATRIX[@]}"; do
		IFS='|' read -r name use_pointer enable_skill <<<"$row"
		printf '%-10s %-10s %-8s\n' "$name" "$([ "$use_pointer" = 1 ] && echo yes || echo no)" "$([ "$enable_skill" = 1 ] && echo enabled || echo disabled)"
	done
	echo
	echo "estimated cost: $ITERATIONS run(s) x 4 configs = $((ITERATIONS * 4)) real codex sessions"
	echo "run without --plan to execute."
	exit 0
fi

mkdir -p "$LOG_DIR" "$BIN_DIR" "$BIN_HEAD_DIR" "$RUN_DIR/projects"

cleanup() {
	if [ -d "$SKILL_HIDDEN" ] && [ ! -d "$SKILL_DIR" ]; then
		mv "$SKILL_HIDDEN" "$SKILL_DIR"
	fi
	rm -rf "$BIN_DIR"
	rm -rf "$BIN_HEAD_DIR"
}
trap cleanup EXIT

if ! command -v "$CODEX_BIN" >/dev/null 2>&1; then
	echo "codex CLI not found: $CODEX_BIN" >&2
	exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
	echo "jq not found on PATH" >&2
	exit 1
fi

echo "building missis from $REPO_DIR"
(cd "$REPO_DIR" && go build -o "$BIN" ./cmd/missis)
# Baseline must run the pre-change CLI: build BASELINE_REF into a separate dir.
git -C "$REPO_DIR" archive "$BASELINE_REF" | tar -x -C "$BIN_HEAD_DIR"
(cd "$BIN_HEAD_DIR" && go build -o "$BIN_HEAD" ./cmd/missis)
prepare_catalog

setup_project() {
	local scratch="$1" use_pointer="$2" missis_bin="$3"
	mkdir -p "$scratch"
	(cd "$scratch" && "$missis_bin" --init --store "$scratch/.missis-store/missis.db" >/dev/null)
	cp "$REPO_DIR/.missis.d/context.md" "$scratch/.missis.d/context.md"
	cp "$REPO_DIR/.missis.d/active.example.md" "$scratch/.missis.d/active.example.md"
	if [ "$use_pointer" = "1" ]; then
		# Hermetic, self-contained pointer fixture. Baseline/skill get no
		# AGENTS.md and never inherit this project's AG1-AG7 instructions.
		printf '%s\n' "$POINTER_AGENTS_MD" >"$scratch/AGENTS.md"
	fi
}

run_config() {
	local name="$1" use_pointer="$2" enable_skill="$3" missis_bin="$4" bin_dir="$5"
	if [ "$enable_skill" = "1" ] && [ -d "$SKILL_HIDDEN" ] && [ ! -d "$SKILL_DIR" ]; then
		mv "$SKILL_HIDDEN" "$SKILL_DIR"
	fi
	if [ "$enable_skill" = "0" ] && [ -d "$SKILL_DIR" ]; then
		mv "$SKILL_DIR" "$SKILL_HIDDEN"
	fi
	for i in $(seq 1 "$ITERATIONS"); do
		local scratch log code start_ns end_ns started_at wall tickets execs turns outcome
		scratch="$RUN_DIR/projects/${name}-${i}"
		rm -rf "$scratch"
		mkdir -p "$scratch"
		log="$LOG_DIR/${name}-${i}.log"
		setup_project "$scratch" "$use_pointer" "$missis_bin"
		started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
		start_ns="$(date +%s%N)"
		code=0
		PATH="$bin_dir:$PATH" timeout 900 "$CODEX_BIN" exec ${CODEX_CATALOG_ARG:-} \
			${CODEX_RUN_ARGS:-} --skip-git-repo-check --ephemeral -C "$scratch" \
			${CODEX_EXTRA_ARGS:-} "create a missis ticket" >"$log" 2>&1 || code=$?
		end_ns="$(date +%s%N)"
		wall="$(awk "BEGIN { printf \"%.1f\", ($end_ns - $start_ns) / 1000000000 }")"
		tickets="$("$BIN" show --json --store "$scratch/.missis-store/missis.db" 2>/dev/null | jq '.tickets | length' 2>/dev/null || echo 0)"
		execs="$(grep -c '^exec$' "$log" || true)"
		turns="$(grep -c '^codex$' "$log" || true)"
		if [ "$tickets" -ge 1 ]; then
			outcome="proceeded"
		else
			outcome="blocked"
		fi
		printf '%s %s %s %s\n' "$PROVIDER_LABEL" "$MODEL_LABEL" "$FAMILY" "$EFFORT_LABEL" >"$LOG_DIR/${name}-${i}.provider"
		printf '%-9s %8s %6s %6s %9s %6s  %s\n' \
			"${name}#${i}" "${wall}s" "$execs" "$turns" "$tickets" "$code" "$outcome"
		RESULT_ROWS+="| ${name}#${i} | $MODEL_LABEL | $started_at | ${wall}s | $execs | $turns | $tickets | $code | $outcome | [log](logs/${name}-${i}.log) |"$'\n'
	done
}

echo "provider: $PROVIDER_LABEL  model: $MODEL_LABEL  family: $FAMILY  effort: $EFFORT_LABEL"
echo "codex: $CODEX_BIN ($("$CODEX_BIN" --version 2>&1 | tail -1))"
if [ -n "$CATALOG_PATCHED" ]; then
echo "catalog: patched to $CATALOG_PATCHED (max->xhigh for CLI compat)"
fi
echo "temp root: $TEMP_ROOT"
echo "logs: $LOG_DIR"
printf '%-9s %8s %6s %6s %9s %6s  %s\n' "config" "wall" "execs" "turns" "tickets" "exit" "outcome"
for row in "${MATRIX[@]}"; do
	IFS='|' read -r name use_pointer enable_skill <<<"$row"
	if [ "$name" = "baseline" ]; then
		run_config "$name" "$use_pointer" "$enable_skill" "$BIN_HEAD" "$BIN_HEAD_DIR"
	else
		run_config "$name" "$use_pointer" "$enable_skill" "$BIN" "$BIN_DIR"
	fi
done

{
	printf '# Agent brief benchmark — %s\n\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	printf -- '- provider: %s\n' "$PROVIDER_LABEL"
	printf -- '- model: %s\n' "$MODEL_LABEL"
	printf -- '- family: %s\n' "$FAMILY"
	printf -- '- effort: %s\n' "$EFFORT_LABEL"
	printf -- '- codex: %s\n' "$("$CODEX_BIN" --version 2>&1 | tail -1)"
	printf -- '- catalog: %s\n' "$(catalog_path)"
	if [ -n "$CATALOG_PATCHED" ]; then
		printf -- '- catalog patch: %s (max -> xhigh)\n' "$CATALOG_PATCHED"
	fi
	printf -- '- baseline ref: %s\n' "$BASELINE_REF"
	printf -- '- iterations per config: %s\n' "$ITERATIONS"
	printf -- '- prompt: create a missis ticket\n\n'
	printf '| config | model | started_at | wall | exec calls | turns | tickets | exit | outcome | log |\n'
	printf '|---|---|---|---|---|---|---|---|---|---|\n'
	printf '%s' "$RESULT_ROWS"
	printf '\nLogs: logs/ (relative to this directory)\n'
} > "$RUN_DIR/results.md"
echo "results: $RUN_DIR/results.md"
if [ "$KEEP" = "1" ]; then
	KEEP_DIR="$REPO_DIR/testsuite/benchmarks/results"
	mkdir -p "$KEEP_DIR"
	KEEP_PATH="$KEEP_DIR/$(basename "$RUN_DIR")"
	mkdir -p "$KEEP_PATH/logs"
	cp -R "$LOG_DIR/." "$KEEP_PATH/logs/"
	cp "$RUN_DIR/results.md" "$KEEP_PATH/results.md"
	if [ -f "$RUN_DIR/models-patched.json" ]; then
		cp "$RUN_DIR/models-patched.json" "$KEEP_PATH/models-patched.json"
	fi
	echo "kept: $KEEP_PATH"
fi

cat <<'EOF'

Notes:
- execs counts `exec` tool calls and turns counts assistant blocks in the
  codex transcript; both are version-specific but stable for codex-cli 0.125.x.
- baseline and pointer run with the missis skill moved aside; skill and brief
  run with it enabled. The skill is restored at exit.
- baseline uses the AGENTS.md and missis binary from HEAD (pre-change), so it
  uses the BASELINE_REF binary and gets no AGENTS.md; pointer/brief get only
  the quick-reference section from this repo, never the full project AGENTS.md.
- a per-run results.md plus raw transcripts under ./temp/run-*/logs map every
  row to its model, iteration, timestamp, and log file; --keep copies the whole
  run folder into testsuite/benchmarks/results/ so it is portable.
EOF
