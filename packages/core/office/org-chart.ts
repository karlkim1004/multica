import type { Issue } from "../types";

// NEX-1072 defect ①: the office board grouped work by `assignee_id` only,
// which is "who this was created for" — not "who is actually holding it
// right now". NEX-1043 already made that a machine-readable field:
// `metadata.waiting_on` is one of "ceo" | "agent:<id>" | "external" | "event"
// (see server/internal/handler/issue_metadata.go). When it's set, it is the
// authoritative current holder; assignee_id is only a fallback for issues
// that haven't been through a wait/blocked cycle yet (most `todo` /
// `in_progress` work never sets waiting_on at all).
const WAITING_ON_AGENT_PREFIX = "agent:";

/** The agent currently holding an issue, or null if no agent holds it. */
export function getEffectiveOwnerAgentId(issue: Issue): string | null {
  const waitingOn = issue.metadata.waiting_on;
  if (typeof waitingOn === "string" && waitingOn.length > 0) {
    return waitingOn.startsWith(WAITING_ON_AGENT_PREFIX)
      ? waitingOn.slice(WAITING_ON_AGENT_PREFIX.length) || null
      : null; // "ceo" / "external" / "event" — no agent holds this right now.
  }
  return issue.assignee_type === "agent" ? issue.assignee_id : null;
}

// NEX-1072 defect ③: an escalation marker is only honest if it traces to a
// real event, not a client-side timer. `waiting_escalation_sweeper.go`
// already runs a real 48h-recall / +24h-reassign SLA on `blocked` /
// `in_review` issues held by an agent, and stamps evidence of each stage
// directly into metadata:
//   - escalation_recalled_at    — set when the 48h recall fires
//   - escalation_reassigned_at  — set when the +24h reassign fires
//   - escalation_reassigned_from — which agent the reassign moved it away from
// This reads those stamps instead of re-deriving elapsed time, so the badge
// only ever shows a stage that actually happened. It intentionally does NOT
// synthesize a tier for todo/in_progress work — there is no SLA sweep for
// those statuses yet, so claiming one would be exactly the decoration NEX-1072
// prohibits. Non-agent-held issues (waiting_on absent, "ceo", "external",
// "event") never carry these stamps and correctly resolve to null.
export type WaitingEscalationTier = "held" | "recalled" | "reassigned";

export function getWaitingEscalationTier(issue: Issue): WaitingEscalationTier | null {
  const waitingOn = issue.metadata.waiting_on;
  if (typeof waitingOn !== "string") return null;
  if (waitingOn === "ceo") {
    return typeof issue.metadata.escalation_reassigned_from === "string" ? "reassigned" : null;
  }
  if (waitingOn.startsWith(WAITING_ON_AGENT_PREFIX)) {
    return typeof issue.metadata.escalation_recalled_at === "string" ? "recalled" : "held";
  }
  return null;
}

/** Agent who the reassign stage moved an issue away from, e.g. "agent:<id>". */
export function getReassignedFromAgentId(issue: Issue): string | null {
  const from = issue.metadata.escalation_reassigned_from;
  return typeof from === "string" && from.startsWith(WAITING_ON_AGENT_PREFIX)
    ? from.slice(WAITING_ON_AGENT_PREFIX.length) || null
    : null;
}

// NEX-1072 mock6 finding: the president's room needs the owner's own held
// tickets (대표님 대기 9건 was measured live), and the owner is a *member*,
// never an agent, so getEffectiveOwnerAgentId (agent-only) can't answer
// this. Mirrors that function's precedence: waiting_on="ceo" is the
// authoritative current holder; assignee_id is the fallback for issues that
// haven't been through a wait cycle (waiting_on unset).
export function isOwnerHeldIssue(issue: Issue, ownerId: string): boolean {
  const waitingOn = issue.metadata.waiting_on;
  if (typeof waitingOn === "string" && waitingOn.length > 0) {
    return waitingOn === "ceo";
  }
  return issue.assignee_type === "member" && issue.assignee_id === ownerId;
}

export function getOwnerHeldIssues(issues: readonly Issue[], ownerId: string): Issue[] {
  return issues.filter((issue) => isOwnerHeldIssue(issue, ownerId));
}

// NEX-1072 mock7 ("티켓 번호를 보여주면 내가 잘 모르잖아"): the office desk
// view must show a human-readable one-line label instead of the issue
// identifier. `metadata.short_label` is an explicit editorial override for
// when the auto-derived label reads awkwardly; otherwise this strips
// leading `[bracket]` tag prefixes (e.g. "[P1/플랫폼] ") off the title and
// caps the remainder to keep desk ticket chips a fixed width. The full
// identifier + title are still available in the tooltip/link — this only
// changes what's visible on the desk itself.
const BRACKET_PREFIX_RE = /^(\s*\[[^\]]*\]\s*)+/;
const SHORT_LABEL_MAX_CHARS = 14;

export function getTicketShortLabel(issue: Issue): string {
  const override = issue.metadata.short_label;
  const raw =
    typeof override === "string" && override.trim().length > 0
      ? override.trim()
      : issue.title.replace(BRACKET_PREFIX_RE, "").trim() || issue.title;
  const chars = Array.from(raw);
  return chars.length > SHORT_LABEL_MAX_CHARS
    ? `${chars.slice(0, SHORT_LABEL_MAX_CHARS).join("")}…`
    : raw;
}

// NEX-1072 whip regression (2026-09-02 CEO live check, "레이아웃 좋네. 챗찍은?"):
// `updated_at` bumps on *any* issue write — a third-party comment, a
// metadata edit, a status flip by another agent — not just an action by the
// actual current holder. In this workspace, issues get near-continuous
// cross-agent commentary (sweepers, QA, the CEO checking in), which reset
// the "how long has this desk been idle" clock every few hours and kept
// maxWaitHours from ever crossing the 12h/24h thresholds live, even when
// `waiting_since` (set atomically with `waiting_on`, see
// issue_metadata.go's requiredWaitingMetadataKeys) showed the same holder
// sitting on it for days — this issue itself (NEX-1072) had
// waiting_since 33h in the past while updated_at was only 9h old. Use
// getWaitStartedAt (waiting_since, falling back to updated_at) as the
// wait-clock start instead; it only moves when ownership actually changes
// hands.
export function getWaitStartedAt(issue: Issue): string {
  const waitingSince = issue.metadata.waiting_since;
  return typeof waitingSince === "string" && waitingSince.length > 0
    ? waitingSince
    : issue.updated_at;
}

// NEX-1072 "states_full" spec (2026-08-30, CEO-approved final): the desk's
// flame/shake/aura/whip visuals scale with how long an agent has actually
// been idle while holding a ticket — the getWaitStartedAt elapsed-hours
// signal `waitBucketClass` already uses for ticket chips, not a synthetic
// countdown. This is a separate, purely additive layer on top of
// `WaitingEscalationTier`/`Severity`: per the issue's "금지" clause, the
// escalation *chain* (department border → team lead → owner alert) must stay
// keyed to real sweeper stamps only, so this intensity never feeds that
// chain — it only decorates the one desk it belongs to.
export type DeskIntensity = 0 | 1 | 2 | 3;

export function getDeskIntensity(idleWithWork: boolean, maxWaitHours: number): DeskIntensity {
  if (!idleWithWork) return 0;
  if (maxWaitHours >= 24) return 3;
  if (maxWaitHours >= 12) return 2;
  if (maxWaitHours >= 4) return 1;
  return 0;
}

// 아이유(TeamLeader) is the one workspace-specific persona this screen is
// allowed to hardcode. The backend already made this exact tradeoff for the
// same agent — `teamLeaderMentionUUID` in server/internal/handler/comment.go
// and `waitingEscalationAuthorAgentID` in
// server/cmd/server/waiting_escalation_sweeper.go both hardcode this same id
// as the platform's default escalation/reroute target. There is no generic
// "team lead" field on Agent/Workspace to read this from instead (only
// per-squad `leader_id` exists, one layer below this). Until such a field
// exists, this constant is the single place a future change (or a second
// workspace needing a different org chart) would need to touch.
export const TEAM_LEADER_AGENT_ID = "a9b0fb13-bfaf-4cea-a6e4-d27e243ec2b0";
