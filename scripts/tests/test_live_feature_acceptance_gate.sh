#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

port="$(( 18000 + RANDOM % 1000 ))"
base_url="http://127.0.0.1:${port}"

cat > "$tmpdir/server.mjs" <<'EOF'
import http from "node:http";

const port = Number(process.env.PORT);
const mode = process.env.MODE ?? "ok";

const required = {
  weekly: '<section data-acceptance="weekly-token-tracker">Weekly token tracker</section>',
  chatInput: '<div data-acceptance="chat-input" contenteditable="true"></div>',
  sendButton: '<button data-acceptance="send-button" aria-label="Send">Send</button>',
  assistant: '<article data-acceptance="assistant-message">assistant response</article>',
  progress: '<div data-acceptance="chat-response-in-progress" aria-live="polite">responding stop</div>',
  headerTokens: '<div data-acceptance="chat-header-token-display">input tokens output tokens</div>',
  refresh: '<button data-acceptance="chat-header-refresh" aria-label="Refresh">Refresh</button>',
  bot: '<button data-acceptance="bot-selector" aria-label="agent picker">agent</button>',
  multi: '<section data-acceptance="multi-chat">multi chat</section>',
  brand: '<a data-acceptance="top-left-brand" aria-label="Multica">Multica</a>',
  gauge: '<meter data-acceptance="llm-token-gauge" aria-label="remaining LLM gauge">remaining gauge</meter>',
};

function omit(key, html) {
  return mode === `missing-${key}` ? "" : html;
}

function page(path) {
  const body = [
    '<html data-theme="dark"><head><link rel="stylesheet" href="/assets/app.css"></head><body>',
    omit("brand", required.brand),
    omit("weekly", required.weekly),
    omit("chatInput", required.chatInput),
    omit("sendButton", required.sendButton),
    omit("assistant", required.assistant),
    omit("progress", required.progress),
    omit("headerTokens", required.headerTokens),
    omit("refresh", required.refresh),
    omit("bot", required.bot),
    omit("multi", required.multi),
    omit("gauge", required.gauge),
    mode === "forbidden-currency" ? "<span>$9</span>" : "",
    `<main>${path}</main></body></html>`,
  ].join("");
  return body;
}

const server = http.createServer((req, res) => {
  if (req.method === "POST" && mode === "method-405") {
    res.writeHead(405);
    res.end("method not allowed");
    return;
  }
  if (req.url === "/assets/app.css") {
    res.writeHead(200, { "content-type": "text/css" });
    res.end(":root{--brand:#0ea5a0;--night:#05070b}");
    return;
  }
  if (req.url === "/favicon.ico") {
    res.writeHead(200, { "content-type": "image/x-icon", "content-length": "4" });
    res.end("icon");
    return;
  }
  res.writeHead(200, { "content-type": "text/html" });
  res.end(page(req.url ?? "/"));
});

server.listen(port, "127.0.0.1", () => {
  console.log(`listening ${port}`);
});
EOF

start_server() {
  local mode="$1"
  MODE="$mode" PORT="$port" node "$tmpdir/server.mjs" > "$tmpdir/server.log" 2>&1 &
  server_pid="$!"
  for _ in {1..50}; do
    if grep -q "listening" "$tmpdir/server.log"; then
      return 0
    fi
    sleep 0.1
  done
  cat "$tmpdir/server.log" >&2
  return 1
}

stop_server() {
  kill "$server_pid" 2>/dev/null || true
  wait "$server_pid" 2>/dev/null || true
}

run_gate() {
  node "$repo_root/scripts/live-feature-acceptance-gate.mjs" \
    --config "$repo_root/scripts/live-acceptance-checks.json" \
    --base-url "$base_url" \
    --no-browser
}

expect_pass() {
  local mode="$1"
  start_server "$mode"
  run_gate > "$tmpdir/$mode.out" 2>&1
  stop_server
}

expect_fail() {
  local mode="$1"
  start_server "$mode"
  if run_gate > "$tmpdir/$mode.out" 2>&1; then
    cat "$tmpdir/$mode.out" >&2
    stop_server
    echo "expected gate failure for $mode" >&2
    exit 1
  fi
  stop_server
}

expect_pass ok
expect_fail forbidden-currency
expect_fail method-405
expect_fail missing-assistant
expect_fail missing-progress
expect_fail missing-headerTokens
expect_fail missing-refresh
expect_fail missing-brand
expect_fail missing-gauge

start_server missing-gauge
if LIVE_ACCEPTANCE_BASE_URL="$base_url" LIVE_ACCEPTANCE_CONFIG="$repo_root/scripts/live-acceptance-checks.json" \
  LIVE_ACCEPTANCE_NO_BROWSER=1 SKIP_IMAGE_LABEL_CHECK=1 \
  DEPLOY_COMMAND="echo deploy" ROLLBACK_COMMAND="echo rollback" TELEGRAM_COMMAND="echo telegram" \
  bash "$repo_root/scripts/deploy-multica-frontend-with-acceptance-gate.sh" test-image NEX-490 \
  > "$tmpdir/deploy.out" 2>&1; then
  cat "$tmpdir/deploy.out" >&2
  stop_server
  echo "expected deploy wrapper to fail and rollback" >&2
  exit 1
fi
stop_server
grep -q "running rollback command" "$tmpdir/deploy.out"
grep -q "telegram" "$tmpdir/deploy.out"

echo "live feature acceptance gate tests passed"
