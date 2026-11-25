#!/usr/bin/env python3
"""Single locked writer for the shared Markdown facts store."""
import argparse
import fcntl
import os
import re
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(os.environ.get("MULTICA_MEMORY_ROOT", "/Users/macmini16g256ssd/nexai/state/multica-memory"))
LOCK = ROOT / ".write.lock"
SCRIPTS = Path(__file__).resolve().parent
INDEXER = SCRIPTS / "memory_index.py"
SEARCHER = SCRIPTS / "memory_search.py"
PYTHON = os.environ.get("MULTICA_MEMORY_PYTHON", sys.executable)
SECRET_PATTERNS = [
    re.compile(r"(?i)\b(?:api[_ -]?key|password|passwd|secret|access[_ -]?token|refresh[_ -]?token|bearer)\b\s*[:=]?\s*\S{6,}"),
    re.compile(r"(?i)\bsk-[A-Za-z0-9_-]{16,}\b"),
    re.compile(r"(?i)\b이수\s*번호\s*[:：]?\s*[A-Za-z0-9-]{4,}\b"),
    re.compile(r"(?<!\d)(?:\d[ -]?){10,18}(?!\d)"),
]


def contains_secret(text):
    return any(pattern.search(text) for pattern in SECRET_PATTERNS)


def write_fact(slug, description, body, auto=False, task_id="", comment_id=""):
    return write_facts([{"slug": slug, "description": description, "body": body,
                         "auto": auto, "task_id": task_id, "comment_id": comment_id}])[0]


def write_facts(candidates):
    ROOT.joinpath("facts").mkdir(parents=True, exist_ok=True)
    with LOCK.open("a", encoding="utf-8") as lock_file:
        fcntl.flock(lock_file, fcntl.LOCK_EX)
        results = []
        created = []
        for candidate in candidates:
            slug = str(candidate.get("slug", ""))
            description = str(candidate.get("description", "")).strip()
            body = str(candidate.get("body", "")).strip()
            if not re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,78}[a-z0-9])?", slug):
                results.append("invalid_slug")
                continue
            if not description or not body:
                results.append("empty")
                continue
            if contains_secret(description + "\n" + body):
                results.append("blocked_secret")
                continue
            target = ROOT / "facts" / (slug + ".md")
            if target.exists() or target in created:
                results.append("duplicate")
                continue
            front = ["---", "name: " + slug, "description: " + description.replace("\n", " ")]
            if candidate.get("auto"):
                task_id = str(candidate.get("task_id", ""))
                comment_id = str(candidate.get("comment_id", ""))
                front += ["auto: true", "task_id: \"" + task_id + "\"", "comment_id: \"" + comment_id + "\""]
            payload = "\n".join(front + ["---", "", body, ""])
            fd, tmp = tempfile.mkstemp(prefix=".fact-", suffix=".tmp", dir=str(target.parent))
            try:
                with os.fdopen(fd, "w", encoding="utf-8") as stream:
                    stream.write(payload)
                    stream.flush()
                    os.fsync(stream.fileno())
                os.replace(tmp, target)
                created.append(target)
                results.append("written")
            finally:
                if os.path.exists(tmp):
                    os.unlink(tmp)
        if created:
            subprocess.run([PYTHON, str(INDEXER)], check=True, timeout=30, stdout=subprocess.DEVNULL)
            if SEARCHER.is_file():
                subprocess.run([PYTHON, str(SEARCHER), "--root", str(ROOT), "--reindex"],
                               check=False, timeout=90, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        return results


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--slug", required=True)
    parser.add_argument("--description", required=True)
    parser.add_argument("--content-file", required=True)
    parser.add_argument("--auto", action="store_true")
    parser.add_argument("--task-id", default="")
    parser.add_argument("--comment-id", default="")
    args = parser.parse_args()
    body = Path(args.content_file).read_text(encoding="utf-8")
    print(write_fact(args.slug, args.description, body, args.auto, args.task_id, args.comment_id))


if __name__ == "__main__":
    main()
