#!/usr/bin/env python3
"""기억 인덱스를 "잘 찾히게" 재생성한다.

대표님 지시(2026-08-31): "기억을 단기·중기·장기로 하고 있어야 이런 걸 스스로 거르지."
대표님 지시(2026-09-16): "기억만 많고 못찾으면 뭐하냐~ 중요한걸 기억해서 잘 찾는게 중요하지.
중요한 것들 역순으로 보호하고 불필요한건 목차에서 빼자!"

평평하게 459건(62KB)을 나열하니 대표님 지시가 함정 기록 사이에 묻혔다.
그래서 목차는 중요도 순으로 3층만 둔다. 파일은 하나도 지우지 않는다.

  1. 대표님 지시·확정 사실  — 전부 목차에 (절대 보호)
  2. 운영 원칙(장기)        — 전부 목차에
  3. 주제별 함정(중기)      — 목차에는 주제 한 줄, 본문 목록은 topics/<주제>.md
  4. 이슈 상태(단기)        — 최신 10건만
  목차 제외: 날짜별 검증 기록·noop 점검 로그 → topics/archive.md
"""
import os, re, glob, datetime

M = os.environ.get("MULTICA_MEMORY_ROOT", "/Users/macmini16g256ssd/nexai/state/multica-memory")
FACTS = os.path.join(M, "facts")
TOPICS = os.path.join(M, "topics")
OUT = os.path.join(M, "MEMORY.md")
DESC_MAX = 70   # 1층 목차 한 줄 설명 길이(글자). 항목은 빠뜨리지 않고 설명만 줄인다(NEX-1651: MEMORY.md 35KB 이하)
PRINCIPLES_HINT = 8   # 2층은 묶음 파일 링크 + 최근 N개 이름만 인덱스에 둔다
SHORT_SHOW = 10

ISSUE_RE = re.compile(r"^nex\d{3,4}[-_]", re.I)

# 대표님 지시·확정 사실. 이름이나 설명에 걸리면 무조건 최상단 보호.
# "대표님"이 설명에 들어갔다고 다 지시는 아니다(기술 함정도 대표님 기기 얘기를 한다).
# 지시·결정·확정을 뜻하는 표현이 붙은 경우만 잡는다.
CEO_RE = re.compile(
    r"대표님\s*(지시|결정|확정|판단|원문|우선|전달|정정|최우선)|대표님이\s*(원|정|확)|"
    r"^ceo-(?!alert|asset)|yeji|toki|karen|정수경|resume|income|job-application|gov-rnd|"
    r"accenture|lg-is-a|memory-over|nvidia-inception|longterm-means|money-path|"
    r"nexai-platform|aido-design|aido-release-versioning|aido-initial-user|"
    r"aido-multiuser|aido-must-not|aido-rbac|jeong-sukyung|kosa-2026", re.I)

# 목차에서 뺄 잡음: 날짜별 일회성 검증·점검 기록
NOISE_RE = re.compile(
    r"^autopilot-2026\d+-\d+-noop|^2026-08-|^nex-\d{3,4}[-_]?|^ci-regression$|"
    r"^gfxinfo|^governance-scan-2026|^inreview-batch|^perf-verification", re.I)

# 중기 주제 분류: 앞에서부터 먼저 걸린 주제로 간다
TOPIC_RULES = [
    ("5ets",   "5ETS 트레이딩·대시보드",      r"5ets|fred|trading|fable-5ets"),
    ("aido",   "AIDO 앱·안드로이드·에뮬레이터", r"aido|android|avd|apk|emulator|adb|phone-needs|call-ai|gradle|jdk|screencap|svg-"),
    ("chain",  "체인·정체·대기·큐·멘션",       r"chain|stagnation|stall|waiting|queue|dispatch|controller|in-?review|route|autopilot|mention|comment|verdict|bulk-recall|self-monitoring|issue-status|issue-updated|usage-limit|model-capacity|no-work|claude-runtime-assignee"),
    ("verify", "검증·테스트·PR·배포",          r"valid|verify|test|ci-|pr-|merge|release|deploy|live-db|rolling|docs-are|fail-closed|publish"),
    ("infra",  "인프라·런타임·토큰·호스트",     r"launchd|daemon|mac-|oci|dashboard|rclone|db-|docker|claude-cli|quota|runtime|token|kyounuks|local-nonprod|watchdog|browse-skill|claudecodeui"),
    ("multica","Multica CLI·권한·거버넌스",    r"multica|attachment|chat-|cli|skill|governance|hook|auto-close|agent-|iu-|wonhee|bot-ticket|virtual-office|no-devops|no-codex|research-target"),
]


def parse(path):
    name = os.path.basename(path)[:-3]
    desc, typ, tier = "", "", ""
    try:
        txt = open(path, encoding="utf-8").read()
    except Exception:
        return None
    m = re.search(r"^---\n(.*?)\n---", txt, re.S)
    if m:
        fm = m.group(1)
        for key in ("description", "type", "tier"):
            mm = re.search(rf"^\s*{key}:\s*(.+)$", fm, re.M)
            if mm:
                v = mm.group(1).strip().strip('"')
                if key == "description": desc = v
                elif key == "type": typ = v
                else: tier = v
    if not desc:
        body = re.sub(r"^---\n.*?\n---\n", "", txt, flags=re.S).strip()
        desc = next((l.strip("# ").strip() for l in body.splitlines() if l.strip()), "")[:100]
    if not tier:
        if typ in ("user", "feedback"):
            tier = "long"
        elif ISSUE_RE.match(name):
            tier = "short"
        else:
            tier = "mid"
    return {"name": name, "desc": desc, "tier": tier, "mtime": os.path.getmtime(path)}


def line(r, rel="facts"):
    d = r["desc"]
    if len(d) > DESC_MAX:
        d = d[:DESC_MAX].rstrip() + "…"
    return f"- [{r['name']}]({rel}/{r['name']}.md) — {d}"


def topic_of(r):
    key = r["name"] + " " + r["desc"]
    for slug, _, pat in TOPIC_RULES:
        if re.search(pat, key, re.I):
            return slug
    return "etc"


def main():
    rows = [r for r in (parse(p) for p in glob.glob(os.path.join(FACTS, "*.md"))) if r]
    rows.sort(key=lambda r: -r["mtime"])

    ceo, principles, noise, short = [], [], [], []
    topics = {slug: [] for slug, _, _ in TOPIC_RULES}
    topics["etc"] = []
    for r in rows:
        if r["tier"] == "short":
            short.append(r)
        elif CEO_RE.search(r["name"] + " " + r["desc"]) and not NOISE_RE.search(r["name"]):
            ceo.append(r)
        elif NOISE_RE.search(r["name"]):
            noise.append(r)
        elif r["tier"] == "long":
            principles.append(r)
        else:
            topics[topic_of(r)].append(r)

    os.makedirs(TOPICS, exist_ok=True)
    titles = {slug: title for slug, title, _ in TOPIC_RULES}
    titles["etc"] = "기타"
    titles["principles"] = "운영 원칙(장기) 전체"
    titles["archive"] = "목차 제외 — 날짜별 검증·점검 기록"
    titles["issues"] = "이슈 상태 전체"
    for slug, items in list(topics.items()) + [("archive", noise), ("issues", short), ("principles", principles)]:
        title = titles[slug]
        body = [f"# {title} ({len(items)}건)", "", "MEMORY.md 목차의 주제 묶음 본문. 최신순.", ""]
        body += [line(r, "../facts") for r in items]
        open(os.path.join(TOPICS, f"{slug}.md"), "w", encoding="utf-8").write("\n".join(body) + "\n")

    now = datetime.datetime.now().strftime("%Y-%m-%d %H:%M")
    L = [f"[기억 인덱스] 총 {len(rows)}건 · {now} 재생성",
         "",
         "**찾는 법**: ①아래 1·2층을 먼저 훑는다 ②작업 주제에 맞는 3층 묶음 파일을 연다 "
         "③그래도 없으면 `grep -ril 키워드 facts/`. 파일은 지우지 않았다.",
         ""]

    L += [f"## 1. 대표님 지시·확정 사실 ({len(ceo)}건) — 절대 보호",
          "다시 묻지 않는다. 판단이 이것과 어긋나면 판단이 틀린 것이다.", ""]
    L += [line(r) for r in ceo]

    L += ["", f"## 2. 운영 원칙 ({len(principles)}건)",
          "같은 실수를 막는 규칙. 무언가를 '안 된다/완료됐다'고 말하기 전에 본다.", ""]
    hint = ", ".join(r["name"] for r in principles[:PRINCIPLES_HINT])
    L.append(f"- [운영 원칙 {len(principles)}건 전체](topics/principles.md) — 최근: {hint}")

    L += ["", "## 3. 주제별 함정·경로 — 해당 작업 전에 묶음 파일을 연다", ""]
    for slug in [s for s, _, _ in TOPIC_RULES] + ["etc"]:
        items = topics[slug]
        if not items:
            continue
        hint = ", ".join(r["name"] for r in items[:3])
        L.append(f"- [{titles[slug]} {len(items)}건](topics/{slug}.md) — 최근: {hint}")

    L += ["", f"## 4. 이슈 상태 (최신 {min(SHORT_SHOW, len(short))}건 / 전체 {len(short)}건)",
          "해당 이슈를 다룰 때만 연다.", ""]
    L += [line(r) for r in short[:SHORT_SHOW]]
    L.append(f"- 나머지 {max(0, len(short) - SHORT_SHOW)}건: [topics/issues.md](topics/issues.md) 또는 이슈키로 grep")

    L += ["", f"목차 제외 {len(noise)}건(날짜별 검증·점검 기록): [topics/archive.md](topics/archive.md)"]

    open(OUT, "w", encoding="utf-8").write("\n".join(L) + "\n")
    mid_total = sum(len(v) for v in topics.values())
    print(f"장기 {len(ceo) + len(principles)} · 중기 {mid_total} · 단기 {len(short)}")


if __name__ == "__main__":
    main()
