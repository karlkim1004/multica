"""Offline Responses fixture for the real Codex app-server isolation check."""

import http.server
import json
import os
import socketserver
import subprocess
import threading

CODEX = "/usr/lib/node_modules/@openai/codex/node_modules/@openai/codex-linux-x64/vendor/x86_64-unknown-linux-musl/bin/codex"


class Responses(http.server.BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_POST(self):
        if self.path != "/v1/responses":
            self.send_error(404)
            return
        try:
            request = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            if request["model"] != "gpt-6-luna":
                self.send_error(422)
                return
        except (KeyError, ValueError, TypeError):
            self.send_error(400)
            return

        item = {
            "type": "message", "id": "msg_boundary", "role": "assistant",
            "status": "completed", "content": [{
                "type": "output_text", "annotations": [],
                "text": "NEX2859 synthetic isolation PASS",
            }],
        }
        response = {
            "id": "resp_final", "object": "response", "status": "completed",
            "model": request["model"], "output": [item],
            "usage": {"input_tokens": 0, "output_tokens": 0, "total_tokens": 0},
        }
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        for event, fields in (
            ("response.created", {"response": {**response, "status": "in_progress", "output": []}}),
            ("response.output_item.added", {"output_index": 0, "item": item}),
            ("response.output_item.done", {"output_index": 0, "item": item}),
            ("response.completed", {"response": response}),
        ):
            self.wfile.write(("data: " + json.dumps({"type": event, **fields}) + "\n\n").encode())


def main():
    if os.getcwd() != "/work" or os.listdir("."):
        raise RuntimeError("QA work directory must be empty")
    os.makedirs(os.environ["CODEX_HOME"], mode=0o700)
    with socketserver.TCPServer(("127.0.0.1", 0), Responses) as server:
        threading.Thread(target=server.serve_forever, daemon=True).start()
        provider = f'http://127.0.0.1:{server.server_address[1]}/v1'
        args = [
            CODEX, "app-server", "--listen", "stdio://",
            "-c", 'sandbox_mode="read-only"', "-c", 'approval_policy="never"',
            "-c", 'model_provider="qa_fixture"',
            "-c", 'model_providers.qa_fixture.name="QA fixture"',
            "-c", f'model_providers.qa_fixture.base_url="{provider}"',
            "-c", 'model_providers.qa_fixture.wire_api="responses"',
        ]
        raise SystemExit(subprocess.run(args, check=False).returncode)


if __name__ == "__main__":
    main()
