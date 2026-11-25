#!/usr/bin/env python3
"""Bounded local multilingual-e5 retrieval with a deterministic BM25 fallback."""
import argparse
import hashlib
import json
import math
import os
import re
import sqlite3
import struct
import sys
import time
from pathlib import Path

MODEL_NAME = "minishlab/potion-multilingual-128M"
MAX_TOP_N = 8
MAX_DOC_CHARS = 6000


def documents(root):
    paths = sorted((root / "facts").glob("*.md"))
    state = root / "state" / "current-state.json"
    if state.is_file():
        try:
            ledger = json.loads(state.read_text(encoding="utf-8"))
            for key, label in (("kis.overseas_etp_enabled", "해외 ETP 거래 신청 가능 로그인 증권사 권한 상태"),
                               ("kis.leverage_etp_education", "레버리지 ETF ETP 사전교육 이수 번호 키체인 등록 상태")):
                record = ledger.get(key)
                if isinstance(record, dict):
                    paths.append((str(state) + "#" + key, label + "\nkey: " + key + "\n" + json.dumps(record, ensure_ascii=False)))
        except (OSError, ValueError):
            pass
        paths.append(state)
    result = []
    for path in paths:
        if isinstance(path, tuple):
            result.append(path)
            continue
        try:
            content = path.read_text(encoding="utf-8")[:MAX_DOC_CHARS]
        except (OSError, UnicodeError):
            continue
        if content.strip():
            result.append((str(path), content))
    return result


def tokens(text):
    text = text.lower()
    words = re.findall(r"[\w가-힣]+", text, flags=re.UNICODE)
    out = []
    for word in words:
        out.append(word)
        if re.fullmatch(r"[가-힣]{3,}", word):
            out.extend(word[i:i + 2] for i in range(len(word) - 1))
    return out


def bm25(docs, query, top_n):
    q = set(tokens(query))
    document_terms = [set(tokens(content)) for _, content in docs]
    document_frequency = {}
    for terms in document_terms:
        for term in terms:
            document_frequency[term] = document_frequency.get(term, 0) + 1
    scored = []
    for index, (path, content) in enumerate(docs):
        terms = tokens(content)
        if not terms:
            continue
        freq = {}
        for term in terms:
            freq[term] = freq.get(term, 0) + 1
        score = 0.0
        for term in q:
            if term not in freq:
                continue
            df = document_frequency.get(term, 0)
            idf = math.log(1.0 + (len(docs) - df + 0.5) / (df + 0.5))
            tf = freq[term] / (freq[term] + 1.2 * (0.25 + 0.75 * len(terms) / 300))
            score += idf * (1.0 + tf)
        if score:
            if any(term in q for term in ("최근", "방금", "latest", "recent")):
                try:
                    target = Path(path.split("#", 1)[0])
                    age_days = max(0.0, (time.time() - target.stat().st_mtime) / 86400.0)
                    score += 40.0 / (1.0 + age_days)
                except OSError:
                    pass
            scored.append((score, path, content))
    scored.sort(key=lambda row: row[0], reverse=True)
    return [{"path": p, "content": c, "score": s} for s, p, c in scored[:min(MAX_TOP_N, top_n)]]


def cache_path(root):
    target = root / "state" / "search-vectors.sqlite"
    target.parent.mkdir(parents=True, exist_ok=True)
    return target


def embedder():
    from fastembed import TextEmbedding
    return TextEmbedding(model_name=MODEL_NAME, threads=2)


def update_index(root, docs):
    try:
        model = embedder()
    except Exception as exc:
        print("BM25: local embedding unavailable (" + type(exc).__name__ + ")", file=sys.stderr)
        return False
    db = sqlite3.connect(str(cache_path(root)), timeout=5)
    try:
        db.execute("CREATE TABLE IF NOT EXISTS vectors(path TEXT PRIMARY KEY, digest TEXT NOT NULL, vector BLOB NOT NULL)")
        current = {path: hashlib.sha256(text.encode("utf-8")).hexdigest() for path, text in docs}
        stale = {row[0] for row in db.execute("SELECT path FROM vectors")} - set(current)
        for path in stale:
            db.execute("DELETE FROM vectors WHERE path=?", (path,))
        missing = [(path, text) for path, text in docs
                   if db.execute("SELECT digest FROM vectors WHERE path=?", (path,)).fetchone() != (current[path],)]
        for start in range(0, len(missing), 32):
            batch = missing[start:start + 32]
            vectors = list(model.passage_embed(["passage: " + text for _, text in batch]))
            for (path, text), vector in zip(batch, vectors):
                raw = struct.pack("%sf" % len(vector), *[float(v) for v in vector])
                db.execute("INSERT OR REPLACE INTO vectors(path,digest,vector) VALUES(?,?,?)",
                           (path, current[path], raw))
            db.commit()
    finally:
        db.close()
    return True


def dense_search(root, docs, query, top_n):
    db = sqlite3.connect(str(cache_path(root)), timeout=1)
    try:
        rows = db.execute("SELECT path,digest,vector FROM vectors").fetchall()
    finally:
        db.close()
    expected = {p: hashlib.sha256(c.encode("utf-8")).hexdigest() for p, c in docs}
    by_path = {path: content for path, content in docs}
    rows = [row for row in rows if row[0] in expected and expected[row[0]] == row[1]]
    if len(rows) < len(docs):
        return None
    model = embedder()
    vector = next(model.query_embed("query: " + query))
    q = [float(v) for v in vector]
    qnorm = math.sqrt(sum(v * v for v in q)) or 1.0
    scored = []
    for path, _, raw in rows:
        n = len(raw) // 4
        doc = struct.unpack("%sf" % n, raw)
        dnorm = math.sqrt(sum(v * v for v in doc)) or 1.0
        score = sum(a * b for a, b in zip(q, doc)) / (qnorm * dnorm)
        scored.append((score, path, by_path[path]))
    scored.sort(key=lambda row: row[0], reverse=True)
    return [{"path": p, "content": c, "score": s} for s, p, c in scored[:min(MAX_TOP_N, top_n)]]


def hybrid_search(root, docs, query, top_n):
    limit = min(MAX_TOP_N, top_n)
    dense = dense_search(root, docs, query, limit)
    if dense is None:
        return None
    lexical = bm25(docs, query, limit)
    by_path = {path: content for path, content in docs}
    scores = {}
    for rank, item in enumerate(dense, 1):
        scores[item["path"]] = scores.get(item["path"], 0.0) + 1.0 / (60 + rank)
    for rank, item in enumerate(lexical, 1):
        # Exact names and Korean action terms are especially useful for the
        # current-state ledger, so lexical evidence gets three votes.
        scores[item["path"]] = scores.get(item["path"], 0.0) + 3.0 / (60 + rank)
    ranked = sorted(scores.items(), key=lambda pair: pair[1], reverse=True)[:limit]
    return [{"path": path, "content": by_path[path], "score": score} for path, score in ranked]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", default=os.environ.get("MULTICA_MEMORY_ROOT", "/Users/macmini16g256ssd/nexai/state/multica-memory"))
    parser.add_argument("--query", default="")
    parser.add_argument("--top-n", type=int, default=8)
    parser.add_argument("--reindex", action="store_true")
    args = parser.parse_args()
    root = Path(args.root)
    docs = documents(root)
    if args.reindex:
        started = time.monotonic()
        ok = update_index(root, docs)
        print(json.dumps({"model": MODEL_NAME, "indexed": len(docs) if ok else 0,
                          "elapsed_seconds": round(time.monotonic() - started, 3), "ready": ok}))
        return
    items = None
    method = "bm25"
    try:
        items = hybrid_search(root, docs, args.query, max(1, min(MAX_TOP_N, args.top_n)))
        if items is not None:
            method = MODEL_NAME
    except Exception as exc:
        print("BM25: embedding query unavailable (" + type(exc).__name__ + ")", file=sys.stderr)
    if items is None:
        items = bm25(docs, args.query, max(1, min(MAX_TOP_N, args.top_n)))
    print(json.dumps({"method": method, "items": items}, ensure_ascii=False))


if __name__ == "__main__":
    main()
