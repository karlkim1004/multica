import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { Issue, IssueStatus } from "../types";

// Office-only data path (NEX-1040 validator FAIL): the shared board query
// (`issueListOptions`) intentionally caps each status bucket at
// `ISSUE_PAGE_SIZE` (50) for the paginated board UI. The office screen's
// "workspace-wide longest wait" and escalation counts must NOT silently
// drop issues past the 50th in a bucket, so this fetches every page per
// status instead of just the first — same shape as
// `fetchProjectGanttIssues` in `../issues/queries.ts`.
export const OFFICE_OPEN_STATUSES: readonly IssueStatus[] = [
  "todo",
  "in_progress",
  "in_review",
  "blocked",
];

// Server clamps `limit` to 100 regardless of what's requested
// (server/internal/handler/issue.go), so this is the largest page size
// that doesn't waste a round trip.
export const OFFICE_PAGE_LIMIT = 100;

// Paranoia cap on the per-status loop. Real workspaces are nowhere near
// this; it exists so a runaway backlog degrades to a truncated-but-honest
// read (logged) instead of an unbounded fetch loop.
export const OFFICE_MAX_ISSUES_PER_STATUS = 5_000;

async function fetchAllIssuesForStatus(status: IssueStatus): Promise<Issue[]> {
  const issues: Issue[] = [];
  let offset = 0;
  while (offset < OFFICE_MAX_ISSUES_PER_STATUS) {
    const res = await api.listIssues({ status, limit: OFFICE_PAGE_LIMIT, offset });
    issues.push(...res.issues);
    // Advance by the actual page length, not the requested limit — the
    // server silently clamps `limit` to 100, so trusting the requested
    // value here would skip pages once a bucket exceeds that clamp.
    offset += res.issues.length;
    if (res.issues.length < OFFICE_PAGE_LIMIT) break;
    if (issues.length >= res.total) break;
  }
  if (offset >= OFFICE_MAX_ISSUES_PER_STATUS) {
    console.warn(
      `office: truncated "${status}" issues at ${OFFICE_MAX_ISSUES_PER_STATUS}`,
    );
  }
  return issues;
}

export async function fetchAllOpenIssuesForOffice(): Promise<Issue[]> {
  const perStatus = await Promise.all(
    OFFICE_OPEN_STATUSES.map((status) => fetchAllIssuesForStatus(status)),
  );
  return perStatus.flat();
}

export const officeKeys = {
  openIssues: (wsId: string) => ["workspaces", wsId, "office", "open-issues"] as const,
};

export function officeOpenIssuesOptions(wsId: string) {
  return queryOptions({
    queryKey: officeKeys.openIssues(wsId),
    queryFn: () => fetchAllOpenIssuesForOffice(),
  });
}
