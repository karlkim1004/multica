import { describe, expect, it } from "vitest";
import type { Issue, IssueMetadata, IssueStatus } from "../types";
import {
  getDeskIntensity,
  getEffectiveOwnerAgentId,
  getOwnerHeldIssues,
  getReassignedFromAgentId,
  getTicketShortLabel,
  getWaitingEscalationTier,
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
