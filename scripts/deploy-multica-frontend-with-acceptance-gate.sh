#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

image="${1:-}"
release="${2:-manual}"

if [[ -z "$image" ]]; then
  echo "usage: $0 <image-tag> [release-id]" >&2
  exit 2
fi

gate_config="${LIVE_ACCEPTANCE_CONFIG:-scripts/live-acceptance-checks.json}"
base_url="${LIVE_ACCEPTANCE_BASE_URL:-https://multica.nexai.co.kr}"
deploy_command="${DEPLOY_COMMAND:-}"
rollback_command="${ROLLBACK_COMMAND:-}"
telegram_command="${TELEGRAM_COMMAND:-}"

notify() {
  local message="$1"
  if [[ -n "$telegram_command" ]]; then
    TELEGRAM_MESSAGE="$message" bash -lc "$telegram_command"
  else
    echo "telegram alert: $message"
  fi
}

rollback() {
  local reason="$1"
  echo "acceptance gate failed: $reason" >&2
  if [[ -n "$rollback_command" ]]; then
    echo "running rollback command"
    ROLLBACK_IMAGE="${PREVIOUS_IMAGE:-}" RELEASE_ID="$release" bash -lc "$rollback_command"
  else
    echo "rollback command missing; deployment remains blocked" >&2
  fi
  notify "NEX-490 live acceptance gate failed for ${release}; rollback attempted"
}

if [[ "${SKIP_IMAGE_LABEL_CHECK:-0}" != "1" ]] && command -v docker >/dev/null 2>&1; then
  trunk_label="$(docker image inspect "$image" --format '{{ index .Config.Labels "multica.production-trunk" }}' 2>/dev/null || true)"
  ref_label="$(docker image inspect "$image" --format '{{ index .Config.Labels "org.opencontainers.image.ref.name" }}' 2>/dev/null || true)"
  if [[ "$trunk_label" != "main" || "$ref_label" != "main" ]]; then
    echo "refusing production deploy: image lacks main trunk labels" >&2
    exit 1
  fi
fi

if [[ ! -f "$gate_config" ]]; then
  echo "refusing production deploy: live acceptance config missing at $gate_config" >&2
  exit 1
fi

if [[ -n "$deploy_command" ]]; then
  IMAGE="$image" RELEASE_ID="$release" bash -lc "$deploy_command"
else
  echo "DEPLOY_COMMAND is empty; gate-only mode for $image"
fi

gate_args=(scripts/live-feature-acceptance-gate.mjs --config "$gate_config" --base-url "$base_url")
if [[ "${LIVE_ACCEPTANCE_NO_BROWSER:-0}" == "1" ]]; then
  gate_args+=(--no-browser)
fi

if ! node "${gate_args[@]}"; then
  rollback "live acceptance checks failed"
  exit 1
fi

echo "live acceptance gate passed for $release"
