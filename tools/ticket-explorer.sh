#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

if [ -n "${MISSIS_BIN:-}" ]; then
  missis_cmd=("$MISSIS_BIN")
else
  missis_cmd=(go run ./cmd/missis)
fi

run_missis() {
  "${missis_cmd[@]}" "$@"
}

usage() {
  cat <<'EOF'
missis ticket explorer

  list                 list tickets
  show <ref>           show a ticket or part subtree
  json <ref>           show the projection as JSON
  md <ref> [file]      export a ticket as Markdown; write to file when given
  refs <ref>           show incoming and outgoing links
  lineage <ref>        traverse the local link graph
  search <query>       search ticket titles and current part text
  help                 show this help
  quit | exit          leave the explorer
EOF
}

while IFS= read -r -p "missis> " line; do
  read -r -a args <<< "$line"
  [ "${#args[@]}" -eq 0 ] && continue
  cmd="${args[0]}"
  case "$cmd" in
    list|tickets)
      run_missis show
      ;;
    show)
      [ "${#args[@]}" -ge 2 ] || { echo "usage: show <ref>" >&2; continue; }
      run_missis show "${args[1]}"
      ;;
    json)
      [ "${#args[@]}" -ge 2 ] || { echo "usage: json <ref>" >&2; continue; }
      run_missis show "${args[1]}" --json
      ;;
    md|markdown)
      [ "${#args[@]}" -ge 2 ] || { echo "usage: md <ref> [file]" >&2; continue; }
      if [ "${#args[@]}" -ge 3 ]; then
        run_missis show "${args[1]}" --format markdown > "${args[2]}"
        echo "wrote ${args[2]}"
      else
        run_missis show "${args[1]}" --format markdown
      fi
      ;;
    refs|references)
      [ "${#args[@]}" -ge 2 ] || { echo "usage: refs <ref>" >&2; continue; }
      run_missis show "${args[1]}" --references
      ;;
    lineage)
      [ "${#args[@]}" -ge 2 ] || { echo "usage: lineage <ref>" >&2; continue; }
      run_missis show "${args[1]}" --lineage
      ;;
    search)
      [ "${#args[@]}" -ge 2 ] || { echo "usage: search <query>" >&2; continue; }
      query="${args[*]:1}"
      run_missis show --search "$query"
      ;;
    help|--help|-h)
      usage
      ;;
    quit|exit)
      exit 0
      ;;
    *)
      echo "unknown command: $cmd" >&2
      usage >&2
      ;;
  esac
done
