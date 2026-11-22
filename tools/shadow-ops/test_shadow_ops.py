import json
import tempfile
import threading
import unittest
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import Request, urlopen

from shadow_ops import ShadowStore, create_server, loopback_host


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

    def test_missing_input_duplicate_and_unmirrored_result_are_rejected(self):
        with self.assertRaises(ValueError):
            self.store.mirror_input({"prompt": "missing id"})
        self.store.mirror_input({"task_id": "one"})
        with self.assertRaises(ValueError):
            self.store.mirror_input({"task_id": "one"})
        with self.assertRaises(ValueError):
            self.store.discard_result({"task_id": "two"})

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
