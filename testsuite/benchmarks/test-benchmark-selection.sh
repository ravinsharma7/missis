#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

codex_home="$tmp/codex-home"
mkdir -p "$codex_home"
cat >"$codex_home/config.toml" <<'EOF'
model_provider = "deepseek"
model = "deepseek-v4-flash"
model_reasoning_effort = "high"
service_tier = "fast"
EOF

plan="$(
  cd "$repo_root"
  CODEX_HOME="$codex_home" testsuite/benchmarks/benchmark-agent-brief.sh --plan --baseline-ref HEAD
)"
printf '%s\n' "$plan" | grep -q 'provider: deepseek  model: deepseek-v4-flash  family: deepseek  effort: high'
printf '%s\n' "$plan" | grep -q 'config mode: isolated'
printf '%s\n' "$plan" | grep -q 'service tier: fast'

manual_plan="$(
  cd "$repo_root"
  CODEX_HOME="$codex_home" \
    CODEX_MODEL_PROVIDER=openai \
    CODEX_MODEL=gpt-manual \
    CODEX_SERVICE_TIER=fast \
    testsuite/benchmarks/benchmark-agent-brief.sh --plan \
      --provider deepseek --model deepseek-manual --service-tier fast --baseline-ref HEAD
)"
printf '%s\n' "$manual_plan" | grep -q 'provider: deepseek  model: deepseek-manual  family: deepseek'
printf '%s\n' "$manual_plan" | grep -q 'service tier: fast'

cat >"$codex_home/config.toml" <<'EOF'
model_provider = "openai"
model = "gpt-config"
model_reasoning_effort = "high"
service_tier = "default"
EOF
none_plan="$(
  cd "$repo_root"
  CODEX_HOME="$codex_home" testsuite/benchmarks/benchmark-agent-brief.sh --plan \
    --config-mode inherit --service-tier none --baseline-ref HEAD
)"
printf '%s\n' "$none_plan" | grep -q 'config mode: inherit'
printf '%s\n' "$none_plan" | grep -q 'service tier: unset'

fake_codex="$tmp/codex"
cat >"$fake_codex" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ "${1:-}" = "--version" ]; then
	 echo "${CODEX_FAKE_VERSION:-codex-test 0.1}"
  exit 0
fi
if [ "${1:-}" = "exec" ]; then
	for arg in "$@"; do
		if [ "$arg" = "--help" ]; then
			printf '%s\n' '--full-auto'
			exit 0
		fi
	done
	if [ -n "${CODEX_FAKE_MARKER:-}" ]; then
		if [ "${2:-}" != "resume" ]; then
			printf '%s\n' "$*" >> "$CODEX_FAKE_MARKER"
		fi
	fi
	if [ "${CODEX_FAKE_SUCCESS:-0}" = "1" ]; then
		if [ "${CODEX_FAKE_JSON:-0}" = "1" ]; then
			printf '%s\n' \
				'{"type":"thread.started","thread_id":"test"}' \
				'{"type":"turn.started"}' \
				'{"type":"item.completed","item":{"type":"command_execution"}}' \
				'{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":2,"cache_write_input_tokens":0,"output_tokens":6,"reasoning_output_tokens":4}}'
		else
			printf '%s\n' 'codex' 'exec' 'tokens used' '42'
		fi
		exit 0
	fi
	exit "${CODEX_FAKE_EXIT:-1}"
fi
exit 0
EOF
chmod +x "$fake_codex"

bad_catalog="$tmp/incompatible-catalog.json"
printf '%s\n' '{"models":[{"slug":"deepseek-v4-flash"}]}' >"$bad_catalog"
cat >"$codex_home/config.toml" <<EOF
model_provider = "deepseek"
model = "deepseek-v4-flash"
model_reasoning_effort = "high"
service_tier = "fast"
model_catalog_json = "$bad_catalog"
EOF

preflight_output="$tmp/preflight.out"
preflight_error="$tmp/preflight.err"
preflight_code=0
(
  cd "$repo_root"
  CODEX_HOME="$codex_home" CODEX_BIN="$fake_codex" \
    testsuite/benchmarks/benchmark-agent-brief.sh --scenario missing-title --baseline-ref HEAD
) >"$preflight_output" 2>"$preflight_error" || preflight_code=$?
if [ "$preflight_code" -ne 2 ]; then
  echo "incompatible catalog preflight exit=$preflight_code" >&2
  cat "$preflight_error" >&2
  exit 1
fi
grep -q 'missing base_instructions' "$preflight_error"

version_catalog="$tmp/version-catalog.json"
printf '%s\n' '{"client_version":"0.149.0","models":[{"slug":"deepseek-v4-flash","base_instructions":"test","supported_reasoning_levels":[{"effort":"high"}]}]}' >"$version_catalog"
cat >"$codex_home/config.toml" <<EOF
model_provider = "deepseek"
model = "deepseek-v4-flash"
model_reasoning_effort = "high"
service_tier = "fast"
model_catalog_json = "$version_catalog"
EOF
version_output="$tmp/version.out"
version_error="$tmp/version.err"
version_code=0
(
  cd "$repo_root"
  CODEX_HOME="$codex_home" CODEX_BIN="$fake_codex" \
    testsuite/benchmarks/benchmark-agent-brief.sh --preflight --baseline-ref HEAD
) >"$version_output" 2>"$version_error" || version_code=$?
if [ "$version_code" -ne 2 ]; then
	 echo "version mismatch preflight exit=$version_code" >&2
	 cat "$version_error" >&2
	 exit 1
fi
grep -q 'version mismatch' "$version_error"

modern_catalog="$tmp/modern-catalog.json"
printf '%s\n' '{"client_version":"0.149.0","models":[{"slug":"deepseek-v4-flash","default_reasoning_level":"high"}]}' >"$modern_catalog"
cat >"$codex_home/config.toml" <<EOF
model_provider = "deepseek"
model = "deepseek-v4-flash"
model_reasoning_effort = "high"
service_tier = "fast"
model_catalog_json = "$modern_catalog"
EOF
modern_output="$tmp/modern.out"
modern_error="$tmp/modern.err"
modern_code=0
(
  cd "$repo_root"
  CODEX_HOME="$codex_home" CODEX_BIN="$fake_codex" CODEX_FAKE_VERSION="codex-test 0.149.0" \
    testsuite/benchmarks/benchmark-agent-brief.sh --preflight --baseline-ref HEAD
) >"$modern_output" 2>"$modern_error" || modern_code=$?
if [ "$modern_code" -ne 0 ]; then
	echo "matching modern catalog preflight exit=$modern_code" >&2
	cat "$modern_error" >&2
	exit 1
fi
grep -q 'preflight only: no model sessions run' "$modern_output"

cat >"$codex_home/config.toml" <<'EOF'
model_provider = "deepseek"
model = "deepseek-v4-flash"
model_reasoning_effort = "high"
service_tier = "fast"
EOF
marker="$tmp/fake-codex-execs"
blocked_output="$tmp/blocked.out"
blocked_error="$tmp/blocked.err"
blocked_code=0
(
  cd "$repo_root"
  CODEX_HOME="$codex_home" CODEX_BIN="$fake_codex" \
    CODEX_FAKE_MARKER="$marker" CODEX_FAKE_EXIT=1 \
    testsuite/benchmarks/benchmark-agent-brief.sh --baseline-ref HEAD
) >"$blocked_output" 2>"$blocked_error" || blocked_code=$?
if [ "$blocked_code" -ne 3 ]; then
  echo "blocked benchmark exit=$blocked_code" >&2
  cat "$blocked_error" >&2
  exit 1
fi
if [ "$(wc -l <"$marker" | tr -d ' ')" -ne 1 ]; then
  echo "blocked benchmark should stop after the first failed session" >&2
  exit 1
fi
grep -q -- '--ignore-user-config' "$marker"
grep -q -- '--model deepseek-v4-flash' "$marker"
grep -q -- 'model_provider="deepseek"' "$marker"
grep -q -- 'service_tier="fast"' "$marker"
grep -q 'benchmark blocked:' "$blocked_error"

canary_marker="$tmp/fake-codex-canary"
canary_output="$tmp/canary.out"
canary_error="$tmp/canary.err"
canary_code=0
(
  cd "$repo_root"
  CODEX_HOME="$codex_home" CODEX_BIN="$fake_codex" \
    CODEX_FAKE_MARKER="$canary_marker" CODEX_FAKE_SUCCESS=1 \
    testsuite/benchmarks/benchmark-agent-brief.sh --canary \
      --scenario missing-title --baseline-ref HEAD
) >"$canary_output" 2>"$canary_error" || canary_code=$?
if [ "$canary_code" -ne 0 ]; then
	 echo "canary benchmark exit=$canary_code" >&2
	 cat "$canary_error" >&2
	 exit 1
fi
if [ "$(wc -l <"$canary_marker" | tr -d ' ')" -ne 1 ]; then
	 echo "canary benchmark should run exactly one model session" >&2
	 exit 1
fi
grep -q 'canary: missing-title/brief' "$canary_output"
grep -q 'preflight: passed' "$canary_output"

json_canary_output="$tmp/json-canary.out"
json_canary_error="$tmp/json-canary.err"
json_canary_code=0
(
  cd "$repo_root"
  CODEX_HOME="$codex_home" CODEX_BIN="$fake_codex" \
    CODEX_FAKE_SUCCESS=1 CODEX_FAKE_JSON=1 \
    testsuite/benchmarks/benchmark-agent-brief.sh --canary \
      --scenario missing-title --baseline-ref HEAD
) >"$json_canary_output" 2>"$json_canary_error" || json_canary_code=$?
if [ "$json_canary_code" -ne 0 ]; then
	echo "JSON canary benchmark exit=$json_canary_code" >&2
	cat "$json_canary_error" >&2
	exit 1
fi
grep -q 'input' "$json_canary_output"
grep -q 'reasoning' "$json_canary_output"
grep -q ' 10 ' "$json_canary_output"
grep -q ' 16 ' "$json_canary_output"

full_marker="$tmp/fake-codex-full"
full_output="$tmp/full.out"
full_error="$tmp/full.err"
full_code=0
(
  cd "$repo_root"
  CODEX_HOME="$codex_home" CODEX_BIN="$fake_codex" \
    CODEX_FAKE_MARKER="$full_marker" CODEX_FAKE_SUCCESS=1 \
    testsuite/benchmarks/benchmark-agent-brief.sh --iterations 2 --baseline-ref HEAD
) >"$full_output" 2>"$full_error" || full_code=$?
if [ "$full_code" -ne 0 ]; then
	echo "full benchmark exit=$full_code" >&2
	cat "$full_error" >&2
	exit 1
fi
if [ "$(wc -l <"$full_marker" | tr -d ' ')" -ne 24 ]; then
	echo "two-iteration benchmark should run exactly 24 sessions including one canary" >&2
	exit 1
fi
grep -q 'canary: missing-title/brief' "$full_output"

workflow_marker="$tmp/fake-codex-workflow"
workflow_output="$tmp/workflow.out"
workflow_error="$tmp/workflow.err"
workflow_code=0
(
  cd "$repo_root"
  CODEX_HOME="$codex_home" CODEX_BIN="$fake_codex" \
    CODEX_FAKE_MARKER="$workflow_marker" CODEX_FAKE_SUCCESS=1 \
    testsuite/benchmarks/benchmark-agent-brief.sh --suite workflow \
      --iterations 1 --baseline-ref HEAD
) >"$workflow_output" 2>"$workflow_error" || workflow_code=$?
if [ "$workflow_code" -ne 0 ]; then
	echo "workflow benchmark exit=$workflow_code" >&2
	cat "$workflow_error" >&2
	exit 1
fi
if [ "$(wc -l <"$workflow_marker" | tr -d ' ')" -ne 20 ]; then
	echo "workflow benchmark should run exactly 20 sessions including one canary" >&2
	exit 1
fi
grep -q 'canary: create-parts/brief' "$workflow_output"
workflow_plan="$(
  cd "$repo_root"
  CODEX_HOME="$codex_home" testsuite/benchmarks/benchmark-agent-brief.sh \
    --plan --suite workflow --baseline-ref HEAD
)"
printf '%s\n' "$workflow_plan" | grep -q 'suite: workflow'
printf '%s\n' "$workflow_plan" | grep -q 'scenario names: create-parts, many-open, note-lifecycle, report-open, followup-title'

echo "benchmark selection tests passed"
