#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${1:-.env.worktree}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if [ ! -f "$ENV_FILE" ]; then
  bash scripts/init-worktree-env.sh "$ENV_FILE"
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a
# shellcheck disable=SC1091
. scripts/local-env.sh

backend_pid=""
frontend_pid=""
stop_tree() {
  local pid="$1" child
  [ -n "$pid" ] || return 0
  for child in $(pgrep -P "$pid" 2>/dev/null || true); do
    stop_tree "$child"
  done
  kill "$pid" 2>/dev/null || true
}
cleanup() {
  stop_tree "$backend_pid"
  stop_tree "$frontend_pid"
  wait "$backend_pid" 2>/dev/null || true
  wait "$frontend_pid" 2>/dev/null || true
  docker compose --env-file "$ENV_FILE" down -v > /dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

make setup ENV_FILE="$ENV_FILE"

(cd server && go run ./cmd/server) > .reconnect-probe-backend.log 2>&1 &
backend_pid=$!
# Browser traffic stays same-origin for the disposable stack.  Next proxies
# /api and /ws to REMOTE_API_URL, which avoids relying on a host CORS setting.
REMOTE_API_URL="http://127.0.0.1:${PORT}" \
NEXT_PUBLIC_API_URL="" \
NEXT_PUBLIC_WS_URL="" \
pnpm -C apps/web exec next dev --turbopack --port "$FRONTEND_PORT" > .reconnect-probe-frontend.log 2>&1 &
frontend_pid=$!

wait_for_http() {
  local url="$1" name="$2"
  for _ in $(seq 1 90); do
    if curl -fsS --max-time 2 "$url" > /dev/null; then
      echo "✓ ${name} HTTP ready: ${url}"
      return 0
    fi
    sleep 2
  done
  echo "✗ ${name} did not become HTTP-ready: ${url}" >&2
  return 1
}

wait_for_http "http://127.0.0.1:${PORT}/health" "Backend"
wait_for_http "$FRONTEND_ORIGIN/login" "Frontend"

E2E_BASE_URL="$FRONTEND_ORIGIN" \
E2E_API_URL="http://127.0.0.1:${PORT}" \
E2E_DATABASE_URL="$DATABASE_URL" \
E2E_JWT_SECRET="$JWT_SECRET" \
npx playwright test e2e/chat-reconnect-probe.spec.ts e2e/chat-session-isolation.spec.ts e2e/chat-realtime.spec.ts --project=chromium
