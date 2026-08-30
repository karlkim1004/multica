import { describe, expect, it } from "vitest";
import type { Issue, IssueMetadata, IssueStatus } from "../types";
import {
  getEffectiveOwnerAgentId,
  getReassignedFromAgentId,
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
