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
  echo "codex-test 0.1"
  exit 0
fi
if [ "${1:-}" = "exec" ]; then
  if [ -n "${CODEX_FAKE_MARKER:-}" ]; then
    printf '%s\n' "$*" >> "$CODEX_FAKE_MARKER"
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

echo "benchmark selection tests passed"
