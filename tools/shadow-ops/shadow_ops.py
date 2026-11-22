#!/usr/bin/env python3
"""Offline shadow-run input mirror, result sink, and comparison dashboard.

This service deliberately has no upstream API client and no claim/complete route.
Inputs must be exported by a read-only source-side process and imported as JSONL.
"""

from __future__ import annotations

import argparse
import hashlib
import ipaddress
import json
import socket
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any


def read_rows(path: Path) -> list[dict[str, Any]]:
    if not path.exists():
        return []
    rows = []
    for line in path.read_text(encoding="utf-8").splitlines():
        if line.strip():
            item = json.loads(line)
            if not isinstance(item, dict):
                raise ValueError("each JSONL row must be an object")
            rows.append(item)
    return rows


def append_row(path: Path, row: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as out:
        out.write(json.dumps(row, sort_keys=True, separators=(",", ":")) + "\n")


class ShadowStore:
    def __init__(self, root: Path):
        self.root = root
        self.inputs = root / "inputs.jsonl"
        self.results = root / "results.jsonl"
        self.lock = threading.Lock()

    def mirror_input(self, item: dict[str, Any]) -> dict[str, Any]:
        task_id = item.get("task_id")
        if not isinstance(task_id, str) or not task_id:
            raise ValueError("task_id is required")
        # Keep payload immutable and mark it as a shadow copy. No claim call occurs.
        clone = json.loads(json.dumps(item))
        clone["shadow"] = True
        clone["claim_allowed"] = False
        with self.lock:
            existing = {r["task_id"]: r for r in read_rows(self.inputs)}
            if task_id in existing:
                return existing[task_id]
            append_row(self.inputs, clone)
        return clone

    def discard_result(self, item: dict[str, Any]) -> dict[str, Any]:
        task_id = item.get("task_id")
        if not isinstance(task_id, str) or not task_id:
            raise ValueError("task_id is required")
        # A result is only accepted for a mirrored input and is written solely here.
        with self.lock:
            allowed = {r["task_id"] for r in read_rows(self.inputs)}
            if task_id not in allowed:
                raise ValueError("result has no mirrored input")
            existing = {r["task_id"] for r in read_rows(self.results)}
            if task_id in existing:
                raise ValueError("result already recorded")
            result = {"task_id": task_id, "status": str(item.get("status", "unknown")),
                      "output": item.get("output"), "shadow": True,
                      "sink": "local_only"}
            append_row(self.results, result)
        return result

    def report(self) -> dict[str, Any]:
        inputs = {r["task_id"]: r for r in read_rows(self.inputs)}
        results = {r["task_id"]: r for r in read_rows(self.results)}
        rows = []
        for task_id in sorted(inputs.keys() | results.keys()):
            source = inputs.get(task_id, {})
            result = results.get(task_id, {})
            expected = source.get("expected_output")
            actual = result.get("output")
            rows.append({"task_id": task_id,
                         "state": "missing_input" if task_id not in inputs else
                                  "awaiting_result" if task_id not in results else
                                  "match" if expected == actual else "diff",
                         "status": result.get("status"),
                         "expected_sha256": digest(expected) if expected is not None else None,
                         "actual_sha256": digest(actual) if actual is not None else None})
        compared = sum(r["state"] in ("match", "diff") for r in rows)
        return {"source_inputs": len(inputs), "shadow_results": len(results),
                "compared": compared, "matches": sum(r["state"] == "match" for r in rows),
                "differences": sum(r["state"] == "diff" for r in rows),
                "pending": sum(r["state"] == "awaiting_result" for r in rows),
                "rows": rows}


def digest(value: Any) -> str:
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":"),
                                     ensure_ascii=False).encode()).hexdigest()


def loopback_host(value: str) -> str:
    """Reject non-loopback binds because the HTTP endpoints have no auth."""
    try:
        address = ipaddress.ip_address(value)
    except ValueError as exc:
        raise ValueError("host must be a loopback IP address") from exc
    if not address.is_loopback:
        raise ValueError("host must be a loopback IP address")
    return str(address)


PAGE = """<!doctype html><meta charset=utf-8><title>Shadow operations</title>
<style>body{font:15px system-ui;max-width:1000px;margin:40px auto;color:#172033}h1{font-size:26px}section{display:flex;gap:12px;margin:20px 0}.card{padding:16px;background:#f1f5f9;border-radius:10px;min-width:110px}.n{font-size:25px;font-weight:700}table{width:100%;border-collapse:collapse}td,th{text-align:left;padding:10px;border-bottom:1px solid #ddd}.match{color:#087443}.diff{color:#b42318}</style>
<h1>Shadow execution comparison</h1><p>Offline mirror · claim disabled · result sink local only</p>
<section id=cards></section><table><thead><tr><th>Task</th><th>Comparison</th><th>Status</th><th>Expected SHA-256</th><th>Shadow SHA-256</th></tr></thead><tbody id=rows></tbody></table>
<script>fetch('/api/report').then(r=>r.json()).then(d=>{const cards=document.querySelector('#cards');for(const k of ['source_inputs','shadow_results','compared','matches','differences','pending']){const c=document.createElement('div');c.className='card';const label=document.createElement('div');label.textContent=k.replace('_',' ');const n=document.createElement('div');n.className='n';n.textContent=d[k];c.append(label,n);cards.append(c)}const body=document.querySelector('#rows');for(const r of d.rows){const tr=document.createElement('tr');for(const v of [r.task_id,r.state,r.status||'',r.expected_sha256||'—',r.actual_sha256||'—']){const td=document.createElement('td');td.textContent=v;if(v===r.state)td.className=r.state;tr.append(td)}body.append(tr)}if(!d.rows.length)body.innerHTML='<tr><td colspan=5>No records imported</td></tr>'})</script>"""


def handler_for(store: ShadowStore):
    class Handler(BaseHTTPRequestHandler):
        def _json(self, status: int, data: Any):
            body = json.dumps(data).encode()
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def do_GET(self):
            if self.path == "/":
                body = PAGE.encode()
                self.send_response(200)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
            elif self.path == "/api/report":
                self._json(200, store.report())
            else:
                self._json(404, {"error": "not found"})

        def do_POST(self):
            if self.path not in ("/shadow/input", "/shadow/result"):
                self._json(404, {"error": "not found; upstream claim/write routes are absent"})
                return
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if length > 2_000_000:
                    raise ValueError("payload too large")
                item = json.loads(self.rfile.read(length))
                if not isinstance(item, dict):
                    raise ValueError("body must be a JSON object")
                value = store.mirror_input(item) if self.path == "/shadow/input" else store.discard_result(item)
                self._json(202, value)
            except (ValueError, json.JSONDecodeError) as exc:
                self._json(400, {"error": str(exc)})

        def log_message(self, format, *args):
            pass
    return Handler


def create_server(host: str, port: int, store: ShadowStore) -> ThreadingHTTPServer:
    """Create a loopback-only server using the address family's socket type."""
    address = ipaddress.ip_address(loopback_host(host))
    server_type = type(
        "LoopbackThreadingHTTPServer",
        (ThreadingHTTPServer,),
        {"address_family": socket.AF_INET6 if address.version == 6 else socket.AF_INET},
    )
    return server_type((str(address), port), handler_for(store))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--data-dir", type=Path, default=Path("./shadow-data"))
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8765)
    args = parser.parse_args()
    try:
        host = loopback_host(args.host)
    except ValueError as exc:
        parser.error(str(exc))
    server = create_server(host, args.port, ShadowStore(args.data_dir))
    print(f"shadow dashboard: http://{host}:{args.port} (claim disabled; local sink)")
    server.serve_forever()


if __name__ == "__main__":
    main()
