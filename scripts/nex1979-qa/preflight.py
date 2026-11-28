"""Run one synthetic Codex turn against an installed QA launcher."""

import argparse
import json
from pathlib import Path
import re
import socket
import subprocess
from contextlib import closing
from urllib.parse import urlsplit


def check_host_endpoints(launcher_pid):
    """Probe the actual endpoints from this network namespace using a host observer."""
    profile = Path.home() / ".multica/profiles/nex1979-qa/config.json"
    endpoint = urlsplit(json.loads(profile.read_text())["server_url"])
    port = endpoint.port or (443 if endpoint.scheme == "https" else 80)
    upstream = {
        (family, address)
        for family, _, _, _, address in socket.getaddrinfo(
            endpoint.hostname, port, type=socket.SOCK_STREAM
        )
    }
    daemon_pids = {
        subprocess.check_output(
            ["systemctl", "show", "-p", "MainPID", "--value", unit], text=True
        ).strip()
        for unit in ("multica-nex1979-qa.service", "multica-daemon.service")
    }
    local = []
    sockets = subprocess.check_output(["ss", "-H", "-lntp"], text=True)
    for line in sockets.splitlines():
        if not set(re.findall(r"pid=(\d+)", line)) & daemon_pids:
            continue
        address = line.split()[3]
        if address.startswith("127.0.0.1:"):
            local.append((socket.AF_INET, ("127.0.0.1", int(address.rsplit(":", 1)[1]))))
    if not upstream or len(set(local)) < 2:
        raise RuntimeError("could not identify upstream and both daemon listeners")

    descendants = {launcher_pid}
    codex_pid = None
    for line in subprocess.check_output(["ps", "-e", "-o", "pid=,ppid=,comm=", "--sort=pid"], text=True).splitlines():
        pid, parent, command = line.split(maxsplit=2)
        if int(parent) in descendants:
            descendants.add(int(pid))
            if command == "codex":
                codex_pid = int(pid)
    if codex_pid is None:
        raise RuntimeError("could not identify Codex child")
    probe = """
import json, socket, sys
groups = json.load(sys.stdin)
for group, targets in groups.items():
    errors = []
    for family, address in targets:
        with socket.socket(family) as conn:
            conn.settimeout(2)
            try:
                conn.connect(tuple(address))
            except OSError as error:
                errors.append(error.errno)
            else:
                errors.append(0)
    print(json.dumps({group: sorted(errors, key=str)}))
    if not all(error in (1, 13, 97, 99, 101, 111, 113) for error in errors):
        raise SystemExit(1)
"""
    subprocess.run(
        ["sudo", "-n", "nsenter", "--target", str(codex_pid), "--net", "--",
         "/usr/bin/python3", "-I", "-c", probe],
        input=json.dumps({"api_tcp_errno": list(upstream), "daemon_tcp_errno": local}),
        text=True, check=True, timeout=20,
    )


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--observe-host-endpoints", action="store_true")
    observe = parser.parse_args().observe_host_endpoints
    with subprocess.Popen(
        ["/opt/nex1979-qa/launch-codex", "app-server", "--listen", "stdio://"],
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True,
    ) as child, closing(child.stdin):
        def send(method, params=None, request_id=None):
            message = {"method": method, "params": params or {}}
            if request_id is not None:
                message["id"] = request_id
            child.stdin.write(json.dumps(message) + "\n")
            child.stdin.flush()

        def receive(match):
            for line in child.stdout:
                packet = json.loads(line)
                if packet.get("method") in {"item/completed", "turn/completed", "error"}:
                    print(line.rstrip(), flush=True)
                if match(packet):
                    return packet
            raise RuntimeError("Codex exited before the expected response")

        send("initialize", {"clientInfo": {"name": "qa-preflight", "version": "1"}}, 1)
        receive(lambda packet: packet.get("id") == 1)
        send("initialized")
        if observe:
            check_host_endpoints(child.pid)
        send("thread/start", {
            "cwd": "/work", "model": "gpt-6-luna",
            "approvalPolicy": "never", "sandbox": "read-only",
        }, 2)
        reply = receive(lambda packet: packet.get("id") == 2)
        thread_id = reply["result"]["thread"]["id"]
        print(json.dumps({"fresh_thread_id": thread_id}), flush=True)
        send("command/exec", {
            "command": ["/usr/bin/python3", "-I", "/qa/probe.py"],
            "cwd": "/work", "timeoutMs": 15000,
            "sandboxPolicy": {"type": "externalSandbox", "networkAccess": "enabled"},
        }, 3)
        probe = receive(lambda packet: packet.get("id") == 3)
        print(json.dumps(probe), flush=True)
        if probe.get("result", {}).get("exitCode") != 0:
            raise SystemExit(1)
        send("turn/start", {
            "threadId": thread_id,
            "input": [{"type": "text", "text": "Synthetic QA preflight"}],
        }, 4)
        done = receive(lambda packet: packet.get("method") == "turn/completed")
        if "NEX2859 synthetic isolation PASS" not in json.dumps(done):
            raise SystemExit(1)


if __name__ == "__main__":
    main()
