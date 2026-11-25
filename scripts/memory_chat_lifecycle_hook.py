#!/usr/bin/env python3
"""Fast Claude/Codex lifecycle hook for shared-memory capture and injection."""
import fcntl
import json
import os
import subprocess
import sys
import time
import uuid
from pathlib import Path

from memory_auto_capture import event_contains_secret

ROOT = Path(os.environ.get("MULTICA_MEMORY_ROOT", "/Users/macmini16g256ssd/nexai/state/multica-memory"))
SCRIPTS = Path(__file__).resolve().parent
CAPTURE = SCRIPTS / "memory_auto_capture.py"
SEARCH = SCRIPTS / "memory_search.py"
QUEUE = ROOT / ".chat-capture-queue"
ENABLED = os.environ.get("MULTICA_SHARED_MEMORY", "1").strip().lower() not in ("0", "false", "off", "no")


def text_of(value):
    if isinstance(value, str):
        return value
    if isinstance(value, list):
        return "\n".join(text_of(item) for item in value)
    if isinstance(value, dict):
        if isinstance(value.get("text"), str):
            return value["text"]
        return text_of(value.get("content", ""))
    return ""


def transcript_messages(path):
    user, assistant = None, None
    try:
        with open(path, encoding="utf-8") as stream:
            for line in stream:
                try:
                    row = json.loads(line)
                except ValueError:
                    continue
                if row.get("type") not in {"user", "human", "assistant", "ai"}:
                    continue
                if row.get("isMeta") or row.get("isSidechain") or row.get("isSynthetic"):
                    continue
                msg = row.get("message")
                role = (msg.get("role") if isinstance(msg, dict) else None) or row.get("type")
                role = {"human": "user", "ai": "assistant"}.get(role, role)
                content = msg.get("content") if isinstance(msg, dict) else row.get("content", "")
                if role == "user" and isinstance(content, list):
                    content = [item for item in content if isinstance(item, dict) and item.get("type") == "text"]
                body = text_of(content)
                if role == "user" and body.strip():
                    assistant = None
                    user = (row.get("uuid") or row.get("id") or "", body.strip(), row.get("timestamp", ""))
                elif role == "assistant" and body.strip():
                    assistant = (row.get("uuid") or row.get("id") or "", body.strip(), row.get("timestamp", ""))
    except (OSError, TypeError):
        return None, None
    return user, assistant


def write_event(event):
    QUEUE.mkdir(parents=True, exist_ok=True)
    name = QUEUE / (str(time.time_ns()) + "-" + uuid.uuid4().hex + ".json")
    lock_path = QUEUE / ".queue.lock"
    with lock_path.open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        name.write_text(json.dumps(event, ensure_ascii=False), encoding="utf-8")
    subprocess.Popen([sys.executable, str(Path(__file__).resolve()), "--worker"],
                     stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL,
                     stderr=subprocess.DEVNULL, start_new_session=True,
                     close_fds=True)


def worker():
    QUEUE.mkdir(parents=True, exist_ok=True)
    with (QUEUE / ".worker.lock").open("a") as worker_lock:
        try:
            fcntl.flock(worker_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return 0
        attempts = {}
        while True:
            time.sleep(30)
            with (QUEUE / ".queue.lock").open("a") as lock:
                fcntl.flock(lock, fcntl.LOCK_EX)
                batch = {}
                for path in sorted(QUEUE.glob("*.json")):
                    if attempts.get(path, 0) >= 3:
                        continue
                    try:
                        batch[path] = json.loads(path.read_text(encoding="utf-8"))
                    except (ValueError, UnicodeError):
                        path.rename(path.with_suffix(".invalid"))
                    except OSError:
                        continue
                if not batch:
                    # Release ownership before allowing another producer to enqueue.
                    fcntl.flock(worker_lock, fcntl.LOCK_UN)
                    return 0

            status = "invalid_capture_response"
            exit_code = None
            try:
                result = subprocess.run([sys.executable, str(CAPTURE)],
                                        input=json.dumps(list(batch.values()), ensure_ascii=False),
                                        text=True, stdout=subprocess.PIPE,
                                        stderr=subprocess.DEVNULL, timeout=240, check=False)
                exit_code = result.returncode
                outcome = json.loads(result.stdout)
                if isinstance(outcome, dict):
                    status = outcome.get("status", status)
            except (OSError, subprocess.TimeoutExpired) as exc:
                status = type(exc).__name__
            except ValueError:
                pass

            acknowledged = exit_code == 0 and status in ("processed", "filtered", "disabled")
            if acknowledged:
                with (QUEUE / ".queue.lock").open("a") as lock:
                    fcntl.flock(lock, fcntl.LOCK_EX)
                    for path in batch:
                        path.unlink(missing_ok=True)
                        attempts.pop(path, None)
            else:
                for path in batch:
                    attempts[path] = attempts.get(path, 0) + 1

            (ROOT / "logs").mkdir(parents=True, exist_ok=True)
            with (ROOT / "logs" / "chat-capture.jsonl").open("a", encoding="utf-8") as log:
                log.write(json.dumps({"events": len(batch), "status": status,
                                      "exit_code": exit_code, "acknowledged": acknowledged,
                                      "written": outcome.get("written", 0) if acknowledged else 0}) + "\n")


def inject(prompt):
    if not ENABLED or not prompt.strip():
        return
    try:
        result = subprocess.run([sys.executable, str(SEARCH), "--root", str(ROOT),
                                 "--query", prompt[:12000], "--top-n", "8"],
                                text=True, stdout=subprocess.PIPE,
                                stderr=subprocess.DEVNULL, timeout=2, check=False)
        data = json.loads(result.stdout) if result.returncode == 0 else {}
    except (OSError, subprocess.TimeoutExpired, ValueError):
        return
    items = data.get("items", [])[:8]
    blocks = []
    budget = 5600
    for item in items:
        content = str(item.get("content", "")).strip()
        path = str(item.get("path", ""))
        block = "- source: " + path + "\n  " + content.replace("\n", "\n  ") + "\n"
        if not content or len(blocks) >= 8:
            continue
        remaining = budget - sum(map(len, blocks))
        if remaining <= 0:
            break
        blocks.append(block[:remaining])
    if blocks:
        context = "Relevant shared memory (verify current state from source):\n" + "".join(blocks)
        print(json.dumps({"hookSpecificOutput": {"hookEventName": "UserPromptSubmit",
                                                   "additionalContext": context}}, ensure_ascii=False))


def main():
    if len(sys.argv) > 1 and sys.argv[1] == "--worker":
        return worker()
    try:
        data = json.load(sys.stdin)
    except (ValueError, OSError):
        return 0
    event_name = data.get("hook_event_name", "")
    if event_name == "UserPromptSubmit":
        inject(str(data.get("prompt", "")))
        return 0
    if event_name != "Stop" or not ENABLED:
        return 0
    # Multica task results are captured by the daemon completion path, which
    # has the authoritative issue/comment ids. This hook handles direct chats.
    if os.environ.get("MULTICA_TASK_ID") or os.environ.get("MULTICA_MEMORY_EXTRACTOR") == "1":
        return 0
    user, assistant = transcript_messages(data.get("transcript_path", ""))
    if not user:
        return 0
    session = str(data.get("session_id", "unknown"))
    user_id = user[0] if user else "no-user"
    event = {"task_id": "chat:" + session + ":" + user_id,
             "comment_id": user_id,
             "timestamp": user[2] or time.strftime("%Y-%m-%dT%H:%M:%S%z"),
             "actor_is_workspace_owner": True,
             "user_message": user[1] if user else "",
             "result_comment": assistant[1] if assistant else ""}
    if event_contains_secret(event):
        return 0
    try:
        write_event(event)
    except OSError:
        pass
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
