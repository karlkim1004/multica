import { describe, expect, it } from "vitest";
import type { Agent, Issue, IssueMetadata, IssueStatus } from "../types";
import type { AgentPresenceDetail } from "../agents/types";
import {
  buildBoard,
  buildWhipCommentContent,
  getBlockedReasonText,
  getDeskIntensity,
  getEffectiveOwnerAgentId,
  getOwnerHeldIssues,
  getReassignedFromAgentId,
  getTicketShortLabel,
  getWaitingEscalationTier,
  getWaitReason,
  getWaitStartedAt,
  isWhipCoolingDown,
  SEVERITY_OK,
  SEVERITY_WARN,
  TEAM_LEADER_AGENT_ID,
  WHIP_COOLDOWN_MS,
} from "./org-chart";

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "NEX-1",
    title: "Issue",
    description: null,
    status: "todo" as IssueStatus,
    priority: "none",
    assignee_type: "agent",
    assignee_id: "assignee-agent",
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {} as IssueMetadata,
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
    ...overrides,
  };
}

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "bot-a",
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    name: "Bot A",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "claude",
    owner_id: null,
    skills: [],
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

const IDLE_PRESENCE: AgentPresenceDetail = {
  availability: "online",
  workload: "idle",
  runningCount: 0,
  queuedCount: 0,
  capacity: 1,
};

describe("getEffectiveOwnerAgentId", () => {
  it("falls back to assignee_id when waiting_on is unset (most todo/in_progress work)", () => {
    const issue = makeIssue({ assignee_type: "agent", assignee_id: "bot-a" });
    expect(getEffectiveOwnerAgentId(issue)).toBe("bot-a");
  });

  it("prefers waiting_on=agent:<id> over a stale assignee_id", () => {
    const issue = makeIssue({
      assignee_type: "agent",
      assignee_id: "original-assignee",
      metadata: { waiting_on: "agent:current-holder" },
    });
    expect(getEffectiveOwnerAgentId(issue)).toBe("current-holder");
  });

  it("attributes to no agent once waiting_on has moved to the ceo queue", () => {
    const issue = makeIssue({
      assignee_type: "agent",
      assignee_id: "original-assignee",
      metadata: { waiting_on: "ceo" },
    });
    expect(getEffectiveOwnerAgentId(issue)).toBeNull();
  });

  it("attributes to no agent for external/event waits", () => {
    expect(
      getEffectiveOwnerAgentId(makeIssue({ metadata: { waiting_on: "external" } })),
    ).toBeNull();
    expect(
      getEffectiveOwnerAgentId(makeIssue({ metadata: { waiting_on: "event" } })),
    ).toBeNull();
  });

  it("ignores a member/squad assignee even without waiting_on", () => {
    const issue = makeIssue({ assignee_type: "member", assignee_id: "user-2" });
    expect(getEffectiveOwnerAgentId(issue)).toBeNull();
  });
});

describe("getWaitingEscalationTier", () => {
  it("returns null when waiting_on is unset — no fabricated tier for todo/in_progress", () => {
    expect(getWaitingEscalationTier(makeIssue())).toBeNull();
  });

  it("returns 'held' once an agent holds the wait but no recall has fired", () => {
    const issue = makeIssue({ metadata: { waiting_on: "agent:bot-a" } });
    expect(getWaitingEscalationTier(issue)).toBe("held");
  });

  it("returns 'recalled' only once the sweeper has actually stamped escalation_recalled_at", () => {
    const issue = makeIssue({
      metadata: { waiting_on: "agent:bot-a", escalation_recalled_at: "2026-08-20T00:00:00Z" },
    });
    expect(getWaitingEscalationTier(issue)).toBe("recalled");
  });

  it("returns 'reassigned' only once the sweeper has actually moved it to the ceo queue", () => {
    const issue = makeIssue({
      metadata: {
        waiting_on: "ceo",
        escalation_reassigned_from: "agent:bot-a",
        escalation_reassigned_at: "2026-08-22T00:00:00Z",
      },
    });
    expect(getWaitingEscalationTier(issue)).toBe("reassigned");
  });

  it("does not claim 'reassigned' for a ceo wait that was never auto-escalated", () => {
    const issue = makeIssue({ metadata: { waiting_on: "ceo" } });
    expect(getWaitingEscalationTier(issue)).toBeNull();
  });
});

describe("getReassignedFromAgentId", () => {
  it("extracts the agent id the reassign stage moved ownership away from", () => {
    const issue = makeIssue({ metadata: { escalation_reassigned_from: "agent:bot-a" } });
    expect(getReassignedFromAgentId(issue)).toBe("bot-a");
  });

  it("returns null when there is no reassign stamp", () => {
    expect(getReassignedFromAgentId(makeIssue())).toBeNull();
  });
});

describe("getOwnerHeldIssues", () => {
  it("includes issues explicitly waiting on the ceo queue", () => {
    const issue = makeIssue({ metadata: { waiting_on: "ceo" } });
    expect(getOwnerHeldIssues([issue], "owner-1")).toEqual([issue]);
  });

  it("excludes a ceo-queue issue once waiting_on has moved to an agent", () => {
    const issue = makeIssue({ metadata: { waiting_on: "agent:bot-a" } });
    expect(getOwnerHeldIssues([issue], "owner-1")).toEqual([]);
  });

  it("falls back to a direct member assignee when waiting_on is unset", () => {
    const issue = makeIssue({ assignee_type: "member", assignee_id: "owner-1" });
    expect(getOwnerHeldIssues([issue], "owner-1")).toEqual([issue]);
  });

  it("does not fall back to assignee_id for a different member", () => {
    const issue = makeIssue({ assignee_type: "member", assignee_id: "someone-else" });
    expect(getOwnerHeldIssues([issue], "owner-1")).toEqual([]);
  });

  it("ignores an agent assignee even without waiting_on", () => {
    const issue = makeIssue({ assignee_type: "agent", assignee_id: "bot-a" });
    expect(getOwnerHeldIssues([issue], "owner-1")).toEqual([]);
  });
});

describe("getWaitStartedAt", () => {
  it("falls back to updated_at when waiting_since is unset", () => {
    const issue = makeIssue({ updated_at: "2026-08-01T00:00:00Z" });
    expect(getWaitStartedAt(issue)).toBe("2026-08-01T00:00:00Z");
  });

  it("prefers waiting_since over a much more recent updated_at", () => {
    // Reproduces the live NEX-1072 whip regression: a third-party comment
    // (sweeper/QA/CEO) bumps updated_at without the current holder taking
    // any action, which must not reset the idle clock.
    const issue = makeIssue({
      updated_at: "2026-09-01T22:27:47Z",
      metadata: { waiting_on: "agent:bot-a", waiting_since: "2026-08-31T22:35:21Z" },
    });
    expect(getWaitStartedAt(issue)).toBe("2026-08-31T22:35:21Z");
  });

  it("ignores a non-string waiting_since", () => {
    const issue = makeIssue({
      updated_at: "2026-08-01T00:00:00Z",
      metadata: { waiting_since: 12345 as unknown as string },
    });
    expect(getWaitStartedAt(issue)).toBe("2026-08-01T00:00:00Z");
  });
});

describe("getDeskIntensity", () => {
  it("is 0 when the agent isn't idle-with-work, no matter how long the wait", () => {
    expect(getDeskIntensity(false, 999)).toBe(0);
  });

  it("is 0 for idle-with-work under 4h", () => {
    expect(getDeskIntensity(true, 3.9)).toBe(0);
  });

  it("is 1 (one flame) from 4h up to 12h", () => {
    expect(getDeskIntensity(true, 4)).toBe(1);
    expect(getDeskIntensity(true, 11.9)).toBe(1);
  });

  it("is 2 (two flames) from 12h up to 24h", () => {
    expect(getDeskIntensity(true, 12)).toBe(2);
    expect(getDeskIntensity(true, 23.9)).toBe(2);
  });

  it("is 3 (whip tier) at 24h and beyond", () => {
    expect(getDeskIntensity(true, 24)).toBe(3);
    expect(getDeskIntensity(true, 200)).toBe(3);
  });
});

describe("getTicketShortLabel", () => {
  it("strips a single leading bracket tag", () => {
    const issue = makeIssue({ title: "[P1/플랫폼] 가상 오피스 2차" });
    expect(getTicketShortLabel(issue)).toBe("가상 오피스 2차");
  });

  it("strips multiple leading bracket tags", () => {
    const issue = makeIssue({ title: "[P1] [5ETS] 대시보드 결함 정리" });
    expect(getTicketShortLabel(issue)).toBe("대시보드 결함 정리");
  });

  it("truncates a long remainder with an ellipsis", () => {
    const issue = makeIssue({ title: "이 제목은 열네 글자보다 훨씬 더 길게 작성되었습니다" });
    const label = getTicketShortLabel(issue);
    expect(Array.from(label).length).toBe(15);
    expect(label.endsWith("…")).toBe(true);
  });

  it("leaves a short bracket-free title untouched", () => {
    const issue = makeIssue({ title: "잡코리아 채용공고" });
    expect(getTicketShortLabel(issue)).toBe("잡코리아 채용공고");
  });

  it("prefers an explicit short_label override", () => {
    const issue = makeIssue({
      title: "[P1/5ETS] 대시보드 4가지 결함 정리",
      metadata: { short_label: "5ETS 대시보드 4결함" },
    });
    expect(getTicketShortLabel(issue)).toBe("5ETS 대시보드 4결함");
  });

  it("falls back to the raw title if stripping brackets empties it", () => {
    const issue = makeIssue({ title: "[P1/플랫폼]" });
    expect(getTicketShortLabel(issue)).toBe("[P1/플랫폼]");
  });
});

describe("getWaitReason", () => {
  it("reads 'verifying' from an in_review ticket — queued for someone else, not idle-holding", () => {
    expect(getWaitReason(makeIssue({ status: "in_review" }))).toBe("verifying");
  });

  it("reads 'blocked' from a blocked ticket", () => {
    expect(getWaitReason(makeIssue({ status: "blocked" }))).toBe("blocked");
  });

  it("defaults to 'implementing' for todo/in_progress", () => {
    expect(getWaitReason(makeIssue({ status: "todo" }))).toBe("implementing");
    expect(getWaitReason(makeIssue({ status: "in_progress" }))).toBe("implementing");
  });
});

describe("getBlockedReasonText", () => {
  it("returns the trimmed blocked_reason metadata", () => {
    const issue = makeIssue({ metadata: { blocked_reason: "  waiting on CEO decision  " } });
    expect(getBlockedReasonText(issue)).toBe("waiting on CEO decision");
  });

  it("returns null when blocked_reason is absent or blank", () => {
    expect(getBlockedReasonText(makeIssue())).toBeNull();
    expect(getBlockedReasonText(makeIssue({ metadata: { blocked_reason: "   " } }))).toBeNull();
  });
});

describe("buildBoard", () => {
  it("does not exempt the team lead from idle-with-work severity — regression guard for the 2026-09-02 CEO directive", () => {
    // "오케스트레이터가 놀면 내가 채찍질" only means something if 아이유's own
    // desk escalates exactly like every other agent's when she's holding a
    // ticket idle. This fixture puts a 30h-idle ticket in her tray via
    // waiting_on and asserts buildBoard gives her the same WARN severity +
    // held/maxWaitHours a rank-and-file agent would get in the same spot —
    // no special-cased exclusion anywhere in the aggregation.
    const teamLeader = makeAgent({ id: TEAM_LEADER_AGENT_ID, name: "아이유(TeamLeader)" });
    const staleIssue = makeIssue({
      id: "issue-tl",
      updated_at: "2026-09-02T00:00:00Z",
      metadata: { waiting_on: `agent:${TEAM_LEADER_AGENT_ID}`, waiting_since: "2026-09-01T00:00:00Z" },
    });
    const board = buildBoard([teamLeader], [staleIssue], [], new Map([[TEAM_LEADER_AGENT_ID, IDLE_PRESENCE]]));
    const entry = board.find((e) => e.agent.id === TEAM_LEADER_AGENT_ID);
    expect(entry).toBeDefined();
    expect(entry!.held).toEqual([staleIssue]);
    expect(entry!.severity).toBe(SEVERITY_WARN);
    expect(entry!.maxWaitHours).toBeGreaterThanOrEqual(24);
  });

  it("computes the same held/severity shape for a non-team-lead agent, as a control", () => {
    const rankAndFile = makeAgent({ id: "bot-a" });
    const staleIssue = makeIssue({
      id: "issue-a",
      updated_at: "2026-09-02T00:00:00Z",
      metadata: { waiting_on: "agent:bot-a", waiting_since: "2026-09-01T00:00:00Z" },
    });
    const board = buildBoard([rankAndFile], [staleIssue], [], new Map([["bot-a", IDLE_PRESENCE]]));
    const entry = board.find((e) => e.agent.id === "bot-a");
    expect(entry?.severity).toBe(SEVERITY_WARN);
    expect(entry?.held).toEqual([staleIssue]);
  });

  it("leaves severity OK for an agent with no held work", () => {
    const agent = makeAgent({ id: "bot-b" });
    const board = buildBoard([agent], [], [], new Map());
    expect(board.find((e) => e.agent.id === "bot-b")?.severity).toBe(SEVERITY_OK);
  });

  it("counts recent done issues toward doneCount7d by assignee_id", () => {
    const agent = makeAgent({ id: "bot-a" });
    const done = makeIssue({ id: "done-1", assignee_type: "agent", assignee_id: "bot-a", status: "done" });
    const board = buildBoard([agent], [], [done], new Map());
    expect(board.find((e) => e.agent.id === "bot-a")?.doneCount7d).toBe(1);
  });
});

describe("isWhipCoolingDown", () => {
  it("is false when the agent has never been whipped", () => {
    expect(isWhipCoolingDown(undefined, Date.now())).toBe(false);
  });

  it("is true just after a whip", () => {
    const now = Date.now();
    expect(isWhipCoolingDown(now, now + 1_000)).toBe(true);
  });

  it("is false once the cooldown window has fully elapsed", () => {
    const now = Date.now();
    expect(isWhipCoolingDown(now, now + WHIP_COOLDOWN_MS + 1)).toBe(false);
  });

  it("is true at exactly the boundary (strict less-than)", () => {
    const now = Date.now();
    expect(isWhipCoolingDown(now, now + WHIP_COOLDOWN_MS)).toBe(false);
    expect(isWhipCoolingDown(now, now + WHIP_COOLDOWN_MS - 1)).toBe(true);
  });
});

describe("buildWhipCommentContent", () => {
  it("puts the translated prefix and an agent mention link on its own paragraph", () => {
    const content = buildWhipCommentContent("CEO whip (visual)", "Bot A", "agent-123");
    expect(content).toBe("CEO whip (visual)\n\n[@Bot A](mention://agent/agent-123)");
  });
});
