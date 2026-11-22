#!/usr/bin/env python3
"""Offline shadow-run input mirror, result sink, and comparison dashboard.

This service deliberately has no upstream API client and no claim/complete route.
Inputs must be exported by a read-only source-side process into the isolated SQLite sink.
"""

from __future__ import annotations

import argparse
import hashlib
import ipaddress
import json
import os
import sqlite3
import socket
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any


def read_rows(path: Path, table: str = "inputs") -> list[dict[str, Any]]:
    if table not in ("inputs", "results", "baselines"):
        raise ValueError("unknown shadow table")
    if not path.exists():
        return []
    with sqlite3.connect(path) as db:
        exists = db.execute(
            "SELECT 1 FROM sqlite_master WHERE type='table' AND name=?", (table,)
        ).fetchone()
        if not exists:
            return []
        return [json.loads(row[0]) for row in db.execute(
            f"SELECT payload FROM {table} ORDER BY task_id"
        )]


class ShadowStore:
    def __init__(self, root: Path):
        root.mkdir(mode=0o700, parents=True, exist_ok=True)
        os.chmod(root, 0o700)
        self.root = root
        self.database = root / "shadow.sqlite3"
        self.inputs = self.database
        self.results = self.database
        self.baselines = self.database
        self.lock = threading.Lock()
        with sqlite3.connect(self.database) as db:
            db.execute("PRAGMA journal_mode=DELETE")
            for table in ("inputs", "results", "baselines"):
                db.execute(f"CREATE TABLE IF NOT EXISTS {table} (task_id TEXT PRIMARY KEY, payload TEXT NOT NULL)")
        os.chmod(self.database, 0o600)

    def _rows(self, table: str) -> list[dict[str, Any]]:
        return read_rows(self.database, table)

    def mirror_input(self, item: dict[str, Any]) -> dict[str, Any]:
        task_id = item.get("task_id")
        if not isinstance(task_id, str) or not task_id:
            raise ValueError("task_id is required")
        # Keep payload immutable and mark it as a shadow copy. No claim call occurs.
        clone = json.loads(json.dumps(item))
        clone["shadow"] = True
        clone["claim_allowed"] = False
        with self.lock:
            with sqlite3.connect(self.database) as db:
                db.execute("INSERT OR IGNORE INTO inputs(task_id,payload) VALUES (?,?)",
                           (task_id, json.dumps(clone, sort_keys=True, separators=(",", ":"))))
                row = db.execute("SELECT payload FROM inputs WHERE task_id=?", (task_id,)).fetchone()
        return json.loads(row[0])

    def discard_result(self, item: dict[str, Any]) -> dict[str, Any]:
        task_id = item.get("task_id")
        if not isinstance(task_id, str) or not task_id:
            raise ValueError("task_id is required")
        # A result is only accepted for a mirrored input and is written solely here.
        with self.lock:
            result = {"task_id": task_id, "status": str(item.get("status", "unknown")),
                      "output": item.get("output"), "shadow": True,
                      "sink": "local_only"}
            with sqlite3.connect(self.database) as db:
                if not db.execute("SELECT 1 FROM inputs WHERE task_id=?", (task_id,)).fetchone():
                    raise ValueError("result has no mirrored input")
                if db.execute("SELECT 1 FROM results WHERE task_id=?", (task_id,)).fetchone():
                    raise ValueError("result already recorded")
                db.execute("INSERT INTO results(task_id,payload) VALUES (?,?)",
                           (task_id, json.dumps(result, sort_keys=True, separators=(",", ":"))))
        return result

    def record_baseline(self, item: dict[str, Any]) -> None:
        task_id = item.get("task_id")
        if not isinstance(task_id, str) or not task_id or "output" not in item:
            raise ValueError("baseline requires task_id and output")
        row = {"task_id": task_id, "output": item["output"]}
        with self.lock:
            inputs = {r["task_id"] for r in self._rows("inputs")}
            if task_id not in inputs:
                raise ValueError("baseline has no mirrored input")
            existing = {r["task_id"]: r for r in self._rows("baselines")}
            if task_id in existing:
                if digest(existing[task_id]["output"]) != digest(row["output"]):
                    raise ValueError("baseline already recorded with different output")
                return
            with sqlite3.connect(self.database) as db:
                db.execute("INSERT INTO baselines(task_id,payload) VALUES (?,?)",
                           (task_id, json.dumps(row, sort_keys=True, separators=(",", ":"))))

    def report(self) -> dict[str, Any]:
        with self.lock:
            inputs = {r["task_id"]: r for r in self._rows("inputs")}
            results = {r["task_id"]: r for r in self._rows("results")}
            baselines = {r["task_id"]: r for r in self._rows("baselines")}
        rows = []
        for task_id in sorted(inputs.keys() | results.keys()):
            source = inputs.get(task_id, {})
            result = results.get(task_id, {})
            if task_id in baselines:
                expected_hash = digest(baselines[task_id]["output"])
            else:
                expected_hash = digest(source["expected_output"]) if "expected_output" in source else None
            actual_hash = digest(result["output"]) if "output" in result else None
            if task_id not in inputs:
                state = "missing_input"
            elif task_id not in results:
                state = "awaiting_result"
            elif expected_hash is None:
                state = "awaiting_baseline"
            else:
                state = "match" if expected_hash == actual_hash else "diff"
            rows.append({"task_id": task_id, "state": state,
                         "status": result.get("status"),
                         "expected_sha256": expected_hash,
                         "actual_sha256": actual_hash})
        compared = sum(r["state"] in ("match", "diff") for r in rows)
        return {"source_inputs": len(inputs), "shadow_results": len(results),
                "compared": compared, "matches": sum(r["state"] == "match" for r in rows),
                "differences": sum(r["state"] == "diff" for r in rows),
                "pending": sum(r["state"] in ("awaiting_result", "awaiting_baseline") for r in rows),
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
            if self.path not in ("/shadow/input", "/shadow/result", "/shadow/baseline"):
                self._json(404, {"error": "not found; upstream claim/write routes are absent"})
                return
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if length > 2_000_000:
                    raise ValueError("payload too large")
                item = json.loads(self.rfile.read(length))
                if not isinstance(item, dict):
                    raise ValueError("body must be a JSON object")
                if self.path == "/shadow/input":
                    value = store.mirror_input(item)
                elif self.path == "/shadow/result":
                    value = store.discard_result(item)
                else:
                    store.record_baseline(item)
                    value = {"task_id": item["task_id"]}
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
    parser.add_argument("--data-dir", type=Path,
                        default=Path.home() / "nexai/state/shadow-sink")
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
