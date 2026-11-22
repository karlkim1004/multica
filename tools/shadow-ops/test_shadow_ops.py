import json
import os
import sqlite3
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import Request, urlopen
from unittest.mock import patch

import exporter
from shadow_ops import ShadowStore, create_server, loopback_host, read_rows
from exporter import export_once, get_config, queued_only


class ShadowOpsTest(unittest.TestCase):
    def test_host_must_be_loopback(self):
        self.assertEqual(loopback_host("127.0.0.1"), "127.0.0.1")
        self.assertEqual(loopback_host("::1"), "::1")
        for host in ("0.0.0.0", "192.0.2.1", "localhost"):
            with self.subTest(host=host), self.assertRaises(ValueError):
                loopback_host(host)

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.store = ShadowStore(self.root)

    def tearDown(self):
        self.tmp.cleanup()

    def test_mirror_and_result_are_local_and_comparable(self):
        source = {"task_id": "task-1", "prompt": "work", "expected_output": "done"}
        clone = self.store.mirror_input(source)
        self.assertEqual(source, {"task_id": "task-1", "prompt": "work", "expected_output": "done"})
        self.assertTrue(clone["shadow"])
        self.assertFalse(clone["claim_allowed"])
        result = self.store.discard_result({"task_id": "task-1", "output": "done", "status": "completed"})
        self.assertEqual("local_only", result["sink"])
        self.assertTrue(self.store.results.is_relative_to(self.root))
        self.assertEqual({"source_inputs": 1, "shadow_results": 1, "compared": 1,
                          "matches": 1, "differences": 0, "pending": 0,
                          "rows": [{"task_id": "task-1", "state": "match", "status": "completed",
                                    "expected_sha256": self.store.report()["rows"][0]["expected_sha256"],
                                    "actual_sha256": self.store.report()["rows"][0]["actual_sha256"]}]},
                         self.store.report())
        self.assertEqual("shadow.sqlite3", self.store.database.name)
        self.assertEqual(0o700, self.root.stat().st_mode & 0o777)
        self.assertEqual(0o600, self.store.database.stat().st_mode & 0o777)
        with sqlite3.connect(self.store.database) as db:
            tables = {row[0] for row in db.execute(
                "SELECT name FROM sqlite_master WHERE type='table'")}
        self.assertTrue({"inputs", "results", "baselines"}.issubset(tables))

    def test_missing_input_duplicate_and_unmirrored_result_are_rejected(self):
        with self.assertRaises(ValueError):
            self.store.mirror_input({"prompt": "missing id"})
        self.store.mirror_input({"task_id": "one"})
        self.assertEqual(self.store.mirror_input({"task_id": "one"})["task_id"], "one")
        with self.assertRaises(ValueError):
            self.store.discard_result({"task_id": "two"})

    def test_exporter_only_selects_unrestricted_queued_tasks(self):
        tasks = [
            {"id": "queued", "status": "queued", "execution_policy": "unrestricted"},
            {"id": "dispatched", "status": "dispatched", "execution_policy": "unrestricted"},
            {"id": "restricted", "status": "queued", "execution_policy": "restricted"},
        ]
        selected = queued_only(tasks)
        self.assertEqual(["queued"], [row["task_id"] for row in selected])
        self.assertEqual("queued", selected[0]["source_status"])
        self.assertEqual("pending", selected[0]["shadow_worker"])

    def test_exporter_requires_https_source_and_loopback_receiver(self):
        config = get_config({
            "MULTICA_API_BASE_URL": "https://multica.example",
            "MULTICA_RUNTIME_ID": "runtime-id",
            "MULTICA_DAEMON_TOKEN": "secret",
            "SHADOW_RECEIVER_URL": "http://[::1]:8765",
        })
        self.assertEqual("secret", config[2])
        for bad in (
            {"MULTICA_API_BASE_URL": "http://multica.example", "MULTICA_RUNTIME_ID": "r", "MULTICA_DAEMON_TOKEN": "t"},
            {"MULTICA_API_BASE_URL": "https://multica.example", "MULTICA_RUNTIME_ID": "r", "MULTICA_DAEMON_TOKEN": "t", "SHADOW_RECEIVER_URL": "http://0.0.0.0:8765"},
        ):
            with self.assertRaises(ValueError):
                get_config(bad)

    def test_exporter_reads_credentials_from_keychain_without_logging_them(self):
        entry = {"server_url": "http://127.0.0.1:8081", "runtime_id": "runtime-id",
                 "workspace_id": "workspace-id", "daemon_token": "secret-value"}
        with patch.dict(os.environ, {}, clear=True), \
             patch.object(exporter, "read_keychain_entry", return_value=entry):
            config = exporter.get_config()
        self.assertEqual("http://127.0.0.1:8081", config[0])
        self.assertEqual("runtime-id", config[1])
        self.assertEqual("secret-value", config[2])
        self.assertEqual("http://127.0.0.1:8765", config[3])

    def test_exporter_gets_snapshot_and_posts_queued_rows_only(self):
        source_state = {"methods": [], "authorization": None}

        class SourceHandler(BaseHTTPRequestHandler):
            def do_GET(self):
                source_state["methods"].append("GET")
                source_state["authorization"] = self.headers.get("Authorization")
                body = json.dumps([
                    {"id": "queued-task", "status": "queued", "execution_policy": "unrestricted"},
                    {"id": "owned-task", "status": "dispatched", "execution_policy": "unrestricted"},
                ]).encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def do_POST(self):
                source_state["methods"].append("POST")
                self.send_response(500)
                self.end_headers()

            def log_message(self, *_args):
                pass

        source = ThreadingHTTPServer(("127.0.0.1", 0), SourceHandler)
        source_thread = threading.Thread(target=source.serve_forever, daemon=True)
        source_thread.start()
        receiver = create_server("127.0.0.1", 0, self.store)
        receiver_thread = threading.Thread(target=receiver.serve_forever, daemon=True)
        receiver_thread.start()
        try:
            counts = export_once(
                f"http://127.0.0.1:{source.server_port}", "runtime", "test-secret",
                f"http://127.0.0.1:{receiver.server_port}",
            )
            self.assertEqual({"snapshot_rows": 2, "queued_candidates": 1,
                              "posted_or_already_mirrored": 1}, counts)
            self.assertEqual(["GET"], source_state["methods"])
            self.assertEqual("Bearer test-secret", source_state["authorization"])
            self.assertEqual(["queued-task"], [r["task_id"] for r in read_rows(self.store.inputs)])
        finally:
            receiver.shutdown()
            receiver_thread.join()
            receiver.server_close()
            source.shutdown()
            source_thread.join()
            source.server_close()

    def test_http_exposes_only_shadow_routes_and_dashboard(self):
        server = create_server("127.0.0.1", 0, self.store)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        base = f"http://127.0.0.1:{server.server_port}"
        try:
            self.assertIn(b"claim disabled", urlopen(base + "/").read())
            payload = json.dumps({"task_id": "http-task", "expected_output": "yes"}).encode()
            req = Request(base + "/shadow/input", data=payload, headers={"Content-Type": "application/json"})
            response = json.loads(urlopen(req).read())
            self.assertFalse(response["claim_allowed"])
            forbidden = Request(base + "/api/daemon/runtimes/x/tasks/claim", data=b"{}")
            with self.assertRaises(HTTPError) as err:
                urlopen(forbidden)
            self.assertEqual(404, err.exception.code)
            report = json.loads(urlopen(base + "/api/report").read())
            self.assertEqual(1, report["pending"])
        finally:
            server.shutdown()
            thread.join()
            server.server_close()

    def test_compares_independently_arriving_baseline_and_shadow_results(self):
        with create_server("127.0.0.1", 0, self.store) as server:
            thread = threading.Thread(target=server.serve_forever)
            thread.start()
            base = f"http://127.0.0.1:{server.server_port}"

            def post(path, task_id, **fields):
                request = Request(base + path, data=json.dumps({"task_id": task_id, **fields}).encode())
                with urlopen(request, timeout=5) as response:
                    self.assertEqual(202, response.status)

            def report():
                with urlopen(base + "/api/report", timeout=5) as response:
                    return json.load(response)

            try:
                with self.assertRaises(HTTPError) as err:
                    post("/shadow/baseline", "unknown", output=None)
                self.assertEqual(400, err.exception.code)

                post("/shadow/input", "same")
                post("/shadow/result", "same", output=None)
                self.assertEqual("awaiting_baseline", report()["rows"][0]["state"])
                self.assertEqual(0, report()["compared"])
                post("/shadow/baseline", "same", output=None)
                post("/shadow/baseline", "same", output=None)
                with self.assertRaises(HTTPError) as err:
                    post("/shadow/baseline", "same", output="changed")
                self.assertEqual(400, err.exception.code)

                post("/shadow/input", "different")
                post("/shadow/baseline", "different", output=True)
                post("/shadow/result", "different", output=1)
                result = report()
                self.assertEqual((2, 1, 1, 0), tuple(
                    result[key] for key in ("compared", "matches", "differences", "pending")
                ))
                self.assertEqual(["diff", "match"], [row["state"] for row in result["rows"]])
                self.assertEqual(2, len(read_rows(self.store.baselines, "baselines")))
            finally:
                server.shutdown()
                thread.join()

    def test_ipv6_loopback_server_binds_and_serves(self):
        server = create_server("::1", 0, self.store)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        base = f"http://[::1]:{server.server_port}"
        try:
            self.assertIn(b"claim disabled", urlopen(base + "/").read())
        finally:
            server.shutdown()
            thread.join()
            server.server_close()


if __name__ == "__main__":
    unittest.main()
