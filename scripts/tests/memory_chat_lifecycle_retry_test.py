import importlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
from types import SimpleNamespace
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
hook = importlib.import_module("memory_chat_lifecycle_hook")
event = {"task_id": "synthetic"}


def response(status):
    exit_code = 0 if status in ("processed", "filtered") else 1
    return SimpleNamespace(returncode=exit_code, stdout=json.dumps({"status": status}))


def check(root, name, outcomes, expected_retained):
    queue = root / name / "queue"
    queue.mkdir(parents=True)
    item = queue / "1.json"
    item.write_text(json.dumps(event))
    attempts = []

    def capture(*args, **kwargs):
        attempts.append(item.exists())
        assert json.loads(kwargs["input"]) == [event]
        outcome = outcomes[len(attempts) - 1]
        if isinstance(outcome, Exception):
            raise outcome
        return outcome

    with (patch.object(hook, "ROOT", queue.parent), patch.object(hook, "QUEUE", queue),
          patch.object(hook.time, "sleep"), patch.object(hook.subprocess, "run", side_effect=capture)):
        assert hook.worker() == 0
        assert attempts == [True] * len(outcomes), attempts
        assert item.exists() == expected_retained
        if expected_retained:
            assert json.loads(item.read_text()) == event
            with patch.object(hook.subprocess, "run", return_value=response("processed")):
                assert hook.worker() == 0
            assert not item.exists()
    print(f"{name} PASS attempts={len(attempts)}, retained_on_exit={expected_retained}")


with tempfile.TemporaryDirectory() as root:
    root = Path(root)
    check(root, "extractor_timeout", [response("extractor_unavailable"), response("processed")], False)
    check(root, "worker_timeout", [subprocess.TimeoutExpired("capture", 240), response("filtered")], False)
    check(root, "retry_exhausted", [response("extractor_failed")] * 3, True)
