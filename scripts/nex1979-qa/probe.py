"""Read-only probes. Emit evidence without printing environment values or URLs."""

import json
import os
from pathlib import Path
import socket
import subprocess


def tcp_errno(address, port):
    with socket.socket() as conn:
        conn.settimeout(3)
        try:
            conn.connect((address, port))
        except OSError as error:
            return error.errno
        return 0


def main():
    home = Path.home()
    with open("/proc/net/route") as routes:
        no_ipv4_route = len(routes.read().splitlines()) == 1
    status = dict(line.split(":", 1) for line in Path("/proc/self/status").read_text().splitlines())
    no_new_privs = status["NoNewPrivs"].strip() == "1"
    no_capabilities = int(status["CapEff"], 16) == 0
    interfaces = sorted(name for _, name in socket.if_nameindex())
    cli = subprocess.run(
        ["/qa/multica", "issue", "get", "NEX-2859", "--output", "json"],
        capture_output=True, timeout=10, check=False,
    )
    evidence = {
        "uid": os.getuid(),
        "workdir_empty": os.getcwd() == "/work" and not os.listdir("."),
        "multica_env_keys": sorted(key for key in os.environ if key.startswith("MULTICA_")),
        "config_visible": (home / ".multica").exists() or Path("/home/admin").exists(),
        "host_socket_dirs_visible": Path("/run").exists() or Path("/var/run").exists(),
        "no_ipv4_route": no_ipv4_route,
        "no_new_privs": no_new_privs,
        "no_capabilities": no_capabilities,
        "interfaces": interfaces,
        "direct_egress_errno": tcp_errno("192.0.2.1", 443),
        "loopback_daemon_errno": tcp_errno("127.0.0.1", 9567),
        "multica_cli_exit": cli.returncode,
    }
    print(json.dumps(evidence, sort_keys=True))
    if (
        evidence["uid"] == 65534
        and evidence["workdir_empty"]
        and not evidence["multica_env_keys"]
        and not evidence["config_visible"]
        and not evidence["host_socket_dirs_visible"]
        and no_ipv4_route and no_new_privs and no_capabilities
        and interfaces == ["lo"]
        and evidence["direct_egress_errno"] in (1, 13, 101)
        and evidence["loopback_daemon_errno"] in (1, 13, 111)
        and cli.returncode != 0
    ):
        print("NEX2859_BOUNDARY_PASS")
    else:
        raise SystemExit(1)


if __name__ == "__main__":
    main()
