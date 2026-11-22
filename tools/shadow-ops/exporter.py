#!/usr/bin/env python3
"""Read-only mirror of queued runtime tasks into the local shadow receiver.

This process only performs GET against Multica and POST to the local shadow
receiver. It never calls claim, start, complete, or other source write routes.
"""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import time
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlsplit
from urllib.request import Request, urlopen


MAX_RESPONSE_BYTES = 8_000_000
KEYCHAIN_SERVICE = "nexai-shadow-runner"


def read_keychain_entry(service: str = KEYCHAIN_SERVICE) -> dict[str, str]:
    try:
        result = subprocess.run(
            ["security", "find-generic-password", "-s", service, "-w"],
            capture_output=True, text=True, check=False,
        )
    except OSError as exc:
        raise ValueError("macOS Keychain is unavailable") from exc
    if result.returncode:
        raise ValueError("shadow runner Keychain item is unavailable")
    try:
        entry = json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise ValueError("shadow runner Keychain item is invalid") from exc
    if not isinstance(entry, dict):
        raise ValueError("shadow runner Keychain item is invalid")
    return entry


def get_config(env: dict[str, str] | None = None) -> tuple[str, str, str, str]:
    use_keychain = env is None
    env = os.environ if env is None else env
    base_url = env.get("MULTICA_API_BASE_URL", "")
    runtime_id = env.get("MULTICA_RUNTIME_ID", "")
    token = env.get("MULTICA_DAEMON_TOKEN", "")
    if use_keychain and not (base_url and runtime_id and token):
        keychain = read_keychain_entry()
        base_url = base_url or keychain.get("server_url", "")
        runtime_id = runtime_id or keychain.get("runtime_id", "")
        token = token or keychain.get("daemon_token", "")
    if not all(isinstance(value, str) for value in (base_url, runtime_id, token)):
        raise ValueError("shadow runner Keychain item has invalid fields")
    base_url = base_url.rstrip("/")
    shadow_url = env.get("SHADOW_RECEIVER_URL", "http://127.0.0.1:8765").rstrip("/")
    if not base_url or not runtime_id or not token:
        raise ValueError("MULTICA_API_BASE_URL, MULTICA_RUNTIME_ID, and MULTICA_DAEMON_TOKEN are required")
    source = urlsplit(base_url)
    if not (source.scheme == "https" or
            (source.scheme == "http" and source.hostname in ("127.0.0.1", "localhost", "::1"))):
        raise ValueError("source API must use HTTPS unless it is local loopback")
    shadow = urlsplit(shadow_url)
    if shadow.scheme != "http" or shadow.hostname not in ("127.0.0.1", "::1"):
        raise ValueError("shadow receiver URL must use a loopback host")
    return base_url, runtime_id, token, shadow_url


def request_json(url: str, *, token: str | None = None, body: dict[str, Any] | None = None) -> Any:
    data = None if body is None else json.dumps(body, separators=(",", ":")).encode("utf-8")
    headers = {"Accept": "application/json"}
    if token is not None:
        headers["Authorization"] = f"Bearer {token}"
    if data is not None:
        headers["Content-Type"] = "application/json"
    req = Request(url, data=data, headers=headers, method="GET" if body is None else "POST")
    with urlopen(req, timeout=15) as response:
        raw = response.read(MAX_RESPONSE_BYTES + 1)
    if len(raw) > MAX_RESPONSE_BYTES:
        raise ValueError("response exceeds 8 MB limit")
    return json.loads(raw)


def queued_only(snapshot: Any) -> list[dict[str, Any]]:
    if not isinstance(snapshot, list):
        raise ValueError("pending endpoint must return a JSON array")
    queued = []
    for task in snapshot:
        if not isinstance(task, dict):
            raise ValueError("pending endpoint contains a non-object task")
        if task.get("status") != "queued":
            continue
        task_id = task.get("id")
        if not isinstance(task_id, str) or not task_id:
            raise ValueError("queued task is missing its id")
        if task.get("execution_policy") != "unrestricted":
            continue
        item = dict(task)
        item["task_id"] = task_id
        item["source_status"] = "queued"
        item["shadow_worker"] = "pending"
        queued.append(item)
    return queued


def export_once(base_url: str, runtime_id: str, token: str, shadow_url: str) -> dict[str, int]:
    pending_url = (f"{base_url}/api/daemon/runtimes/{quote(runtime_id, safe='')}/tasks/pending")
    snapshot = request_json(pending_url, token=token)
    queued = queued_only(snapshot)
    posted = 0
    for task in queued:
        request_json(f"{shadow_url}/shadow/input", body=task)
        posted += 1
    return {"snapshot_rows": len(snapshot), "queued_candidates": len(queued), "posted_or_already_mirrored": posted}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--interval", type=float, default=30.0, help="poll interval in seconds; 0 runs once")
    args = parser.parse_args()
    if args.interval < 0:
        parser.error("--interval must be zero or positive")
    try:
        config = get_config()
    except ValueError as exc:
        parser.error(str(exc))
    while True:
        try:
            print(json.dumps(export_once(*config), sort_keys=True), flush=True)
        except (HTTPError, URLError, TimeoutError, ValueError, json.JSONDecodeError) as exc:
            # Do not include URLs, response bodies, or credentials in logs.
            print(json.dumps({"error": type(exc).__name__}), flush=True)
        if args.interval == 0:
            return
        time.sleep(args.interval)


if __name__ == "__main__":
    main()
