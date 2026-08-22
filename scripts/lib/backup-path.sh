#!/usr/bin/env bash

# Print the backup directory for a store path.
#
# An explicit MISSIS_BACKUP_DIR may be absolute or relative to the project
# containing the store. Without an override, keep backups separate from the
# live database and avoid creating a visible backups/ directory in the
# repository: <project>/.missis-backups/.
resolve_missis_backup_dir() {
  local store_path="${1:?store path is required}"
  local store_dir project_dir override

  store_dir="$(cd "$(dirname "$store_path")" && pwd)"
  if [ "$(basename "$store_dir")" = ".missis-store" ]; then
    project_dir="$(dirname "$store_dir")"
  else
    project_dir="$store_dir"
  fi

  override="${MISSIS_BACKUP_DIR:-}"
  if [ -n "$override" ]; then
    if [[ "$override" = /* ]]; then
      printf '%s\n' "$override"
    else
      printf '%s/%s\n' "$project_dir" "$override"
    fi
  else
    printf '%s/.missis-backups\n' "$project_dir"
  fi
}
