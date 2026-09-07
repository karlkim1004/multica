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

// NEX-1072: "ranked by performance, not backlog size" needs a done-issue
// window, separate from the open-issue fetch above. Same all-pages-of-one-
// status shape as fetchAllIssuesForStatus, but filtered server-side by
// date_field/date_start so a 7-day window doesn't require paging through a
// workspace's entire done history.
export const OFFICE_DONE_LOOKBACK_DAYS = 7;

async function fetchDoneIssuesSince(sinceIso: string, endIso: string): Promise<Issue[]> {
  const issues: Issue[] = [];
  let offset = 0;
  while (offset < OFFICE_MAX_ISSUES_PER_STATUS) {
    const res = await api.listIssues({
      status: "done",
      // The server rejects the request with a 400 unless date_field,
      // date_start, AND date_end arrive together (parseIssueDateFilter),
      // which zeroed every desk's doneCount7d when date_end was omitted.
      date_field: "updated_at",
      date_start: sinceIso,
      date_end: endIso,
      limit: OFFICE_PAGE_LIMIT,
      offset,
    });
    issues.push(...res.issues);
    offset += res.issues.length;
    if (res.issues.length < OFFICE_PAGE_LIMIT) break;
    if (issues.length >= res.total) break;
  }
  return issues;
}

// Recomputes the "since" bound on every call (not hoisted to a module
// constant) so each poll tick / refetch slides the 7-day window forward
// instead of freezing it at first render.
export async function fetchRecentDoneIssuesForOffice(): Promise<Issue[]> {
  const now = Date.now();
  const sinceIso = new Date(
    now - OFFICE_DONE_LOOKBACK_DAYS * 24 * 3_600_000,
  ).toISOString();
  return fetchDoneIssuesSince(sinceIso, new Date(now).toISOString());
}

export const officeKeys = {
  openIssues: (wsId: string) => ["workspaces", wsId, "office", "open-issues"] as const,
  recentDone: (wsId: string) => ["workspaces", wsId, "office", "recent-done"] as const,
};

export function officeOpenIssuesOptions(wsId: string) {
  return queryOptions({
    queryKey: officeKeys.openIssues(wsId),
    queryFn: () => fetchAllOpenIssuesForOffice(),
  });
}

export function officeRecentDoneOptions(wsId: string) {
  return queryOptions({
    queryKey: officeKeys.recentDone(wsId),
    queryFn: () => fetchRecentDoneIssuesForOffice(),
  });
}
