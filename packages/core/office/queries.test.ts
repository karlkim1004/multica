import { afterEach, describe, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { Issue, IssueStatus, ListIssuesParams, ListIssuesResponse } from "../types";
import { OFFICE_PAGE_LIMIT, fetchAllOpenIssuesForOffice } from "./queries";

function makeIssue(status: IssueStatus, idx: number): Issue {
  return {
    id: `${status}-${idx}`,
    workspace_id: "ws-1",
    number: idx,
    identifier: `${status.toUpperCase()}-${idx}`,
    title: `Issue ${idx}`,
    description: null,
    status,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: idx,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
  };
}

function installFakeApi(listIssues: (params?: ListIssuesParams) => Promise<ListIssuesResponse>) {
  setApiInstance({ listIssues } as unknown as ApiClient);
}

describe("fetchAllOpenIssuesForOffice", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  // Direct repro of the validator FAIL: the office screen's global max-wait
  // and escalation counts must include every open issue, not just the first
  // page. 150 todo issues forces >1 page at the server's 100-item clamp —
  // the 51st issue is exactly the one the old ISSUE_PAGE_SIZE=50 board query
  // would have silently dropped.
  it("captures every issue in an oversized status bucket, including the 51st and the last page", async () => {
    const todoIssues = Array.from({ length: 150 }, (_, i) => makeIssue("todo", i + 1));
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockImplementation(async (params) => {
        if (params?.status !== "todo") return { issues: [], total: 0 };
        const offset = params.offset ?? 0;
        const limit = Math.min(params.limit ?? OFFICE_PAGE_LIMIT, 100);
        return { issues: todoIssues.slice(offset, offset + limit), total: todoIssues.length };
      });
    installFakeApi(listIssues);

    const result = await fetchAllOpenIssuesForOffice();
    const todoResult = result.filter((issue) => issue.status === "todo");

    expect(todoResult).toHaveLength(150);
    expect(todoResult.some((issue) => issue.identifier === "TODO-51")).toBe(true);
    expect(todoResult.some((issue) => issue.identifier === "TODO-100")).toBe(true);
    expect(todoResult.some((issue) => issue.identifier === "TODO-150")).toBe(true);

    const todoCalls = listIssues.mock.calls.filter(([params]) => params?.status === "todo");
    expect(todoCalls.length).toBeGreaterThan(1);
  });

  it("stops paging once total is reached instead of firing a trailing empty request", async () => {
    const blockedIssues = Array.from({ length: 3 }, (_, i) => makeIssue("blocked", i + 1));
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockImplementation(async (params) => {
        if (params?.status !== "blocked") return { issues: [], total: 0 };
        return { issues: blockedIssues, total: blockedIssues.length };
      });
    installFakeApi(listIssues);

    await fetchAllOpenIssuesForOffice();

    const blockedCalls = listIssues.mock.calls.filter(([params]) => params?.status === "blocked");
    expect(blockedCalls).toHaveLength(1);
  });

  it("fetches all four open statuses in parallel, excluding done/cancelled/backlog", async () => {
    const seenStatuses: (IssueStatus | undefined)[] = [];
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockImplementation(async (params) => {
        seenStatuses.push(params?.status);
        return { issues: [], total: 0 };
      });
    installFakeApi(listIssues);

    await fetchAllOpenIssuesForOffice();

    expect(new Set(seenStatuses)).toEqual(
      new Set(["todo", "in_progress", "in_review", "blocked"]),
    );
  });
});
