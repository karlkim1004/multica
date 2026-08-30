"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import type { Agent } from "@multica/core/types";
import {
  useWorkspacePresenceMap,
  type AgentPresenceDetail,
} from "@multica/core/agents";
import { officeOpenIssuesOptions } from "@multica/core/office";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { StatusIcon } from "../../issues/components/status-icon";
import { useT } from "../../i18n";

// NEX-1040/NEX-1045 "virtual office" screen — static render + polling, no
// realtime stream (explicit ban from the NEX-1045 spike decision: this is a
// "who's idle while work waits" control surface, not a live animation).
const POLL_MS = 30_000;
// Matches scoreboard.py's STALE_H concept but at the 7-day "abandoned"
// threshold the virtual-office design calls out, not the 2h scoreboard one.
const STALE_HOURS = 24 * 7;
const TASK_LIST_LIMIT = 30;

function hoursSince(iso: string): number {
  return (Date.now() - new Date(iso).getTime()) / 3_600_000;
}

function formatWaitHours(hours: number): string {
  if (hours < 24) return `${Math.round(hours)}h`;
  return `${Math.floor(hours / 24)}d`;
}

interface OpenLoad {
  count: number;
  maxWaitHours: number;
}

interface AgentBoardEntry {
  agent: Agent;
  presence: AgentPresenceDetail | null;
  openCount: number;
  maxWaitHours: number;
}

export function OfficePage() {
  const { t } = useT("office");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();

  const { data: agents = [] } = useQuery({
    ...agentListOptions(wsId),
    refetchInterval: POLL_MS,
  });
  // NEX-1040 validator FAIL: the shared board query caps each status bucket
  // at 50, which silently dropped issues (and the true global max-wait)
  // past the 50th. officeOpenIssuesOptions pages through every open issue
  // instead — see packages/core/office/queries.ts.
  const { data: issues = [] } = useQuery({
    ...officeOpenIssuesOptions(wsId),
    refetchInterval: POLL_MS,
  });
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);

  // Server sort_by has no updated_at option, so "longest waiting" is a
  // client-side sort over the fully-paged issue list. Sorted oldest-first,
  // so openIssues[0]'s wait IS the workspace-wide max — the metric the
  // validator FAIL flagged as missing from the old scoreboard badge
  // (ready_max_wait_hours).
  const openIssues = useMemo(
    () =>
      issues
        .slice()
        .sort(
          (a, b) =>
            new Date(a.updated_at).getTime() - new Date(b.updated_at).getTime(),
        ),
    [issues],
  );

  const maxWaitHours = openIssues.length
    ? hoursSince(openIssues[0]!.updated_at)
    : 0;
  const unassignedCount = openIssues.filter((issue) => !issue.assignee_id).length;

  const openByAgent = useMemo(() => {
    const map = new Map<string, OpenLoad>();
    for (const issue of openIssues) {
      if (issue.assignee_type !== "agent" || !issue.assignee_id) continue;
      const wait = hoursSince(issue.updated_at);
      const existing = map.get(issue.assignee_id);
      if (existing) {
        existing.count += 1;
        existing.maxWaitHours = Math.max(existing.maxWaitHours, wait);
      } else {
        map.set(issue.assignee_id, { count: 1, maxWaitHours: wait });
      }
    }
    return map;
  }, [openIssues]);

  const board: AgentBoardEntry[] = useMemo(
    () =>
      agents
        .filter((agent) => !agent.archived_at)
        .map((agent) => {
          const open = openByAgent.get(agent.id);
          return {
            agent,
            presence: presenceMap.get(agent.id) ?? null,
            openCount: open?.count ?? 0,
            maxWaitHours: open?.maxWaitHours ?? 0,
          };
        }),
    [agents, openByAgent, presenceMap],
  );

  // Rule B (NEX-1045 "채찍질 규칙"): idle while holding open work — the
  // orchestrator's recall target. Not scoped to id/persona (that would
  // hardcode one workspace's org chart into a shared product screen); this
  // is the count the escalation summary answers, independent of who acts on it.
  const idleWithTaskCount = board.filter(
    (entry) => entry.openCount > 0 && entry.presence?.workload !== "working",
  ).length;

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="justify-between px-5">
        <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
        {openIssues.length > 0 && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground">
            {t(($) => $.page.max_wait, { wait: formatWaitHours(maxWaitHours) })}
          </span>
        )}
      </PageHeader>
      {/* Escalation summary (NEX-1045 "오케스트레이터 존", counts only — see
          PR notes on why no single persona is pinned here). */}
      <div className="flex items-center gap-4 border-b border-border/60 px-5 py-2 text-xs text-muted-foreground">
        <span>{t(($) => $.page.rule_a, { count: unassignedCount })}</span>
        <span>{t(($) => $.page.rule_b, { count: idleWithTaskCount })}</span>
      </div>
      <div className="flex flex-1 min-h-0 overflow-hidden">
        <div className="w-80 shrink-0 overflow-y-auto border-r border-border/60 p-3">
          <h2 className="mb-2 px-1 text-xs font-medium text-muted-foreground">
            {t(($) => $.page.tasks_heading)}
          </h2>
          {openIssues.length === 0 ? (
            <p className="px-1 text-xs text-muted-foreground">
              {t(($) => $.page.tasks_empty)}
            </p>
          ) : (
            <ul className="space-y-0.5">
              {openIssues.slice(0, TASK_LIST_LIMIT).map((issue) => (
                <li key={issue.id}>
                  <AppLink
                    href={paths.issueDetail(issue.id)}
                    className="flex items-start gap-2 rounded-md px-2 py-1.5 text-xs transition-colors hover:bg-accent"
                  >
                    <StatusIcon status={issue.status} className="mt-0.5 h-3.5 w-3.5" />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-mono text-[10px] text-muted-foreground">
                        {issue.identifier}
                      </span>
                      <span className="block truncate">{issue.title}</span>
                      <span className="mt-0.5 flex items-center gap-1 text-[10px] text-muted-foreground">
                        {issue.assignee_type && issue.assignee_id ? (
                          <>
                            <ActorAvatar
                              actorType={issue.assignee_type}
                              actorId={issue.assignee_id}
                              size={14}
                            />
                          </>
                        ) : (
                          <span>{t(($) => $.page.unassigned)}</span>
                        )}
                        <span>
                          {t(($) => $.page.waited_hours, {
                            hours: Math.round(hoursSince(issue.updated_at)),
                          })}
                        </span>
                      </span>
                    </span>
                  </AppLink>
                </li>
              ))}
            </ul>
          )}
        </div>
        <div className="flex-1 overflow-y-auto p-4">
          <h2 className="mb-3 text-xs font-medium text-muted-foreground">
            {t(($) => $.page.agents_heading)}
          </h2>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(96px,1fr))] gap-4">
            {board.map(({ agent, presence, openCount, maxWaitHours: agentMaxWait }) => {
              const working = presence?.workload === "working";
              const stale = openCount > 0 && agentMaxWait >= STALE_HOURS;
              // Design rule from NEX-1045: an idle agent with open work is a
              // warning, never rendered as restful — only a true zero-open
              // agent gets the quiet "resting" treatment.
              const warn = openCount > 0 && !working;
              const ringClass = working
                ? "ring-success"
                : stale
                  ? "ring-destructive"
                  : warn
                    ? "ring-warning"
                    : "ring-border/50";
              const loadLabel =
                openCount === 0
                  ? t(($) => $.page.resting)
                  : t(($) => $.page.load, {
                      count: openCount,
                      wait: formatWaitHours(agentMaxWait),
                    });
              return (
                <div
                  key={agent.id}
                  className="flex flex-col items-center gap-1 text-center"
                >
                  <div
                    className={cn(
                      "relative rounded-full ring-2 ring-offset-2 ring-offset-background",
                      ringClass,
                    )}
                  >
                    <ActorAvatar actorType="agent" actorId={agent.id} size={48} />
                    {warn && (
                      <span
                        className={cn(
                          "absolute -right-0.5 -top-0.5 flex h-4 w-4 items-center justify-center rounded-full text-white",
                          stale ? "bg-destructive" : "bg-warning",
                        )}
                      >
                        <AlertTriangle className="h-2.5 w-2.5" />
                      </span>
                    )}
                  </div>
                  <span className="max-w-[96px] truncate text-[11px]">
                    {agent.name}
                  </span>
                  <span className="text-[10px] text-muted-foreground">
                    {loadLabel}
                  </span>
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
