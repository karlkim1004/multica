#!/usr/bin/env bash
set -euo pipefail

# A release is complete only after all three steps below succeed. In
# particular, never print the completion marker after a failed migration.
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.selfhost.yml}"
BACKEND_PORT="${BACKEND_PORT:-8080}"
MAX_OPEN_PR_HOURS="${MAX_OPEN_PR_HOURS:-72}"

compose() {
  docker compose -f "$COMPOSE_FILE" "$@"
}

warn_old_open_prs() {
  if ! command -v gh >/dev/null 2>&1; then
    echo "WARN: open-PR age check skipped (gh is unavailable)" >&2
    return
  fi

  local cutoff old_prs
  cutoff="$(date -u -v-"${MAX_OPEN_PR_HOURS}"H '+%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || date -u -d "${MAX_OPEN_PR_HOURS} hours ago" '+%Y-%m-%dT%H:%M:%SZ')"
  old_prs="$(gh pr list --state open --json number,createdAt --jq ".[] | select(.createdAt < \"${cutoff}\") | .number" 2>/dev/null || true)"
  if [[ -n "$old_prs" ]]; then
    echo "WARN: open PR older than ${MAX_OPEN_PR_HOURS}h: $(tr '\n' ' ' <<<"$old_prs")" >&2
  fi
}

warn_old_open_prs

echo '==> [1/3] Applying migrations'
compose run --rm migration

echo '==> [2/3] Recreating release services'
compose up -d --force-recreate backend frontend

echo '==> [3/3] Waiting for migration-aware readiness'
for _ in $(seq 1 30); do
  if curl -fsS "http://localhost:${BACKEND_PORT}/readyz" >/dev/null; then
    echo 'RELEASE COMPLETE: migration, restart, and readiness passed.'
    exit 0
  fi
  sleep 2
done

echo 'RELEASE BLOCKED: readiness did not pass after restart.' >&2
exit 1
