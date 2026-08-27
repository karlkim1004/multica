"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle } from "lucide-react";
import type { Agent } from "@multica/core/types";
import {
  useWorkspacePresenceMap,
  type AgentPresenceDetail,
} from "@multica/core/agents";
import { issueListOptions } from "@multica/core/issues";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { AppLink } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";

// NEX-1040/NEX-1045 "virtual office" screen — static render + polling, no
// realtime stream (explicit ban from the NEX-1045 spike decision: this is a
// "who's idle while work waits" control surface, not a live animation).
// Not wired into the nav menu yet — NEX-1045 defers that until the screen
// itself is reviewed.
const POLL_MS = 30_000;
const OPEN_STATUSES = ["todo", "in_progress", "in_review", "blocked"] as const;
// Matches scoreboard.py's STALE_H concept but at the 7-day "abandoned"
// threshold the virtual-office design calls out, not the 2h scoreboard one.
const STALE_HOURS = 24 * 7;
const TASK_LIST_LIMIT = 30;

function hoursSince(iso: string): number {
  return (Date.now() - new Date(iso).getTime()) / 3_600_000;
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
  const { data: issues = [] } = useQuery({
    ...issueListOptions(wsId),
    refetchInterval: POLL_MS,
  });
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);

  // Server sort_by has no updated_at option, so "longest waiting" is a
  // client-side sort over the already-fetched board buckets.
  const openIssues = useMemo(
    () =>
      issues
        .filter((issue) =>
          (OPEN_STATUSES as readonly string[]).includes(issue.status),
        )
        .slice()
        .sort(
          (a, b) =>
            new Date(a.updated_at).getTime() - new Date(b.updated_at).getTime(),
        ),
    [issues],
  );

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

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="px-5">
        <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
      </PageHeader>
      <div className="flex flex-1 min-h-0 overflow-hidden">
        <div className="w-72 shrink-0 overflow-y-auto border-r border-border/60 p-3">
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
                    className="flex flex-col gap-0.5 rounded-md px-2 py-1.5 text-xs transition-colors hover:bg-accent"
                  >
                    <span className="truncate font-mono text-[10px] text-muted-foreground">
                      {issue.identifier}
                    </span>
                    <span className="truncate">{issue.title}</span>
                    <span className="text-[10px] text-muted-foreground">
                      {t(($) => $.page.waited_hours, {
                        hours: Math.round(hoursSince(issue.updated_at)),
                      })}
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
            {board.map(({ agent, presence, openCount, maxWaitHours }) => {
              const working = presence?.workload === "working";
              const stale = openCount > 0 && maxWaitHours >= STALE_HOURS;
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
              const statusLabel =
                openCount === 0
                  ? t(($) => $.page.resting)
                  : stale
                    ? t(($) => $.page.stale_days, {
                        days: Math.floor(maxWaitHours / 24),
                      })
                    : working
                      ? null
                      : t(($) => $.page.idle_with_task);
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
                  {statusLabel && (
                    <span className="text-[10px] text-muted-foreground">
                      {statusLabel}
                    </span>
                  )}
                </div>
              );
            })}
          </div>
        </div>
      </div>
    </div>
  );
}
