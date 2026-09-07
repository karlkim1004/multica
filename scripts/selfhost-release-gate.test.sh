#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE="$ROOT_DIR/scripts/selfhost-release-gate.sh"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

mkdir -p "$TMP_DIR/bin"
cat >"$TMP_DIR/bin/docker" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >>"$CALLS"
if [[ "$*" == *" run --rm migration"* ]]; then exit "${MIGRATION_EXIT:-0}"; fi
EOF
cat >"$TMP_DIR/bin/curl" <<'EOF'
#!/usr/bin/env bash
exit "${READY_EXIT:-0}"
EOF
cat >"$TMP_DIR/bin/sleep" <<'EOF'
#!/usr/bin/env bash
:
EOF
chmod +x "$TMP_DIR/bin/docker" "$TMP_DIR/bin/curl" "$TMP_DIR/bin/sleep"

run_gate() {
  PATH="$TMP_DIR/bin:$PATH" CALLS="$TMP_DIR/calls" "$GATE" >"$TMP_DIR/out" 2>"$TMP_DIR/err"
}

: >"$TMP_DIR/calls"
export MIGRATION_EXIT=1 READY_EXIT=0
if run_gate; then
  echo 'expected migration failure to block release' >&2
  exit 1
fi
grep -Fq 'run --rm migration' "$TMP_DIR/calls"
if grep -Fq 'up -d --force-recreate' "$TMP_DIR/calls"; then
  echo 'restart ran after failed migration' >&2
  exit 1
fi
if grep -Fq 'RELEASE COMPLETE' "$TMP_DIR/out"; then
  echo 'failed migration was marked complete' >&2
  exit 1
fi

: >"$TMP_DIR/calls"
export MIGRATION_EXIT=0 READY_EXIT=0
run_gate
grep -Fq 'run --rm migration' "$TMP_DIR/calls"
grep -Fq 'up -d --force-recreate backend frontend' "$TMP_DIR/calls"
grep -Fq 'RELEASE COMPLETE: migration, restart, and readiness passed.' "$TMP_DIR/out"

echo 'selfhost release gate tests passed'
