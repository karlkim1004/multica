# Shadow operations

Offline shadow-run receiver and comparison dashboard. The receiver has no
upstream URL, credentials, daemon runtime identity, or route for
claiming/completing source tasks. `exporter.py` polls the authenticated,
read-only pending endpoint using GET, copies only `queued` unrestricted rows,
and ignores `dispatched` rows because those are already owned. It never invokes
claim, start, complete, or other source write routes. Repeated polls are
idempotent by task ID.

Run with `python3 tools/shadow-ops/shadow_ops.py --data-dir /path/to/shadow-data`.
The receiver only accepts loopback IP bind addresses (`127.0.0.1` or `::1`);
non-loopback binds are rejected because its HTTP endpoints do not authenticate.
Open `http://127.0.0.1:8765`. Send shadow outputs to `/shadow/result`. Inputs
and results are persisted as separate JSONL files in the chosen data directory;
results are never forwarded to the source service. A separate read-only results
export can submit the source output to the receiver's `POST /shadow/baseline`
as `{"task_id":"task-1","output":null}` after the input has been mirrored. Use the
same task ID for both outputs. The receiver accepts identical baseline retries
and rejects conflicting replacements. `/api/report` compares canonical JSON
hashes, uses an inline `expected_output` when no separate baseline exists, and
keeps tasks pending until both outputs exist. An explicit JSON null is an output.

Run a one-shot mirror after starting the receiver:

```sh
MULTICA_API_BASE_URL=https://multica.example \
MULTICA_RUNTIME_ID=<runtime-id> \
MULTICA_DAEMON_TOKEN=<daemon-token> \
python3 tools/shadow-ops/exporter.py --interval 0
```

For continuous snapshots use `--interval 30` (seconds). Credentials are read
from the environment and are never printed. The exporter is read-only against
the source; it sends records only to the loopback receiver. The current worker
boundary is the local input/result sink: task execution still must be provided
by an isolated shadow runtime before operational comparisons can be claimed.
