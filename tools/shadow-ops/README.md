# Shadow operations

Offline shadow-run receiver and comparison dashboard. It has no upstream URL,
credentials, daemon runtime identity, or route for claiming/completing source tasks.
The source-side exporter must use a read-only queue snapshot API and POST each
task payload to `/shadow/input`; do not export by calling the daemon claim API.

Run with `python3 tools/shadow-ops/shadow_ops.py --data-dir /path/to/shadow-data`.
The receiver only accepts loopback IP bind addresses (`127.0.0.1` or `::1`);
non-loopback binds are rejected because its HTTP endpoints do not authenticate.
Open `http://127.0.0.1:8765`. Send shadow outputs to `/shadow/result`. Inputs
and results are persisted as separate JSONL files in the chosen data directory;
results are never forwarded to the source service. `/api/report` compares
`expected_output` from each input with the matching shadow output and presents
per-task match/diff/pending state and hashes.

The source read-only exporter and production-like runtime have not been wired in
this issue. Until they are, this tool proves sink isolation and the comparison
path locally, not that production task payloads are being mirrored.
