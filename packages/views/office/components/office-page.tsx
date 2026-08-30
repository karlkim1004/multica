"use client";

import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { AlertTriangle, Crown } from "lucide-react";
import type { Agent, Issue, Squad } from "@multica/core/types";
import {
  useWorkspacePresenceMap,
  type AgentPresenceDetail,
} from "@multica/core/agents";
import {
  officeOpenIssuesOptions,
  officeRecentDoneOptions,
  getEffectiveOwnerAgentId,
  getWaitingEscalationTier,
  getReassignedFromAgentId,
  TEAM_LEADER_AGENT_ID,
} from "@multica/core/office";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
  squadMembersOptions,
} from "@multica/core/workspace/queries";
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
const TASK_LIST_LIMIT = 30;
const HELD_TASKS_SHOWN = 4;

function hoursSince(iso: string): number {
  return (Date.now() - new Date(iso).getTime()) / 3_600_000;
}

function formatWaitHours(hours: number): string {
  if (hours < 24) return `${Math.round(hours)}h`;
  return `${Math.floor(hours / 24)}d`;
}

// NEX-1072 escalation severity, worst-first. Every level above OK traces to
// a real signal — open work held while not working, or an actual NEX-1043
// sweeper stamp (see packages/core/office/org-chart.ts) — never a client-side
// timer standing in for one, per the "no decoration-only whip" requirement.
type Severity = 0 | 1 | 2 | 3;
const SEVERITY_OK: Severity = 0;
const SEVERITY_WARN: Severity = 1;
const SEVERITY_RECALLED: Severity = 2;
const SEVERITY_REASSIGNED: Severity = 3;

interface AgentBoardEntry {
  agent: Agent;
  presence: AgentPresenceDetail | null;
  /** Open issues this agent is the *current* holder of, oldest-first. */
  held: Issue[];
  maxWaitHours: number;
  doneCount7d: number;
  /** This agent previously held a wait the sweeper had to reassign away. */
  reassignHistory: boolean;
  severity: Severity;
}

function buildBoard(
  agents: Agent[],
  openIssuesOldestFirst: Issue[],
  doneIssues: Issue[],
  presenceMap: Map<string, AgentPresenceDetail>,
): AgentBoardEntry[] {
  const heldByAgent = new Map<string, Issue[]>();
  const recalledAgentIds = new Set<string>();
  const reassignHistoryAgentIds = new Set<string>();

  for (const issue of openIssuesOldestFirst) {
    const ownerId = getEffectiveOwnerAgentId(issue);
    if (ownerId) {
      const list = heldByAgent.get(ownerId);
      if (list) list.push(issue);
      else heldByAgent.set(ownerId, [issue]);
      if (getWaitingEscalationTier(issue) === "recalled") recalledAgentIds.add(ownerId);
    }
    if (getWaitingEscalationTier(issue) === "reassigned") {
      const from = getReassignedFromAgentId(issue);
      if (from) reassignHistoryAgentIds.add(from);
    }
  }

  const doneCountByAgent = new Map<string, number>();
  for (const issue of doneIssues) {
    if (issue.assignee_type !== "agent" || !issue.assignee_id) continue;
    doneCountByAgent.set(issue.assignee_id, (doneCountByAgent.get(issue.assignee_id) ?? 0) + 1);
  }

  return agents
    .filter((agent) => !agent.archived_at)
    .map((agent) => {
      const held = heldByAgent.get(agent.id) ?? [];
      const presence = presenceMap.get(agent.id) ?? null;
      const working = presence?.workload === "working";
      const maxWaitHours = held.reduce(
        (max, issue) => Math.max(max, hoursSince(issue.updated_at)),
        0,
      );
      const reassignHistory = reassignHistoryAgentIds.has(agent.id);
      // Design rule from NEX-1045/NEX-1072: idle with open work is a
      // warning, never rendered as restful. Real sweeper stamps escalate it
      // further — they only exist for blocked/in_review agent-held waits,
      // so most todo/in_progress work tops out at WARN, honestly.
      let severity: Severity = SEVERITY_OK;
      if (held.length > 0 && !working) severity = SEVERITY_WARN;
      if (recalledAgentIds.has(agent.id)) severity = SEVERITY_RECALLED;
      if (reassignHistory) severity = SEVERITY_REASSIGNED;
      return {
        agent,
        presence,
        held,
        maxWaitHours,
        doneCount7d: doneCountByAgent.get(agent.id) ?? 0,
        reassignHistory,
        severity,
      };
    });
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
  // NEX-1072 condition 4: rank by recent throughput, not backlog size.
  const { data: doneIssues = [] } = useQuery({
    ...officeRecentDoneOptions(wsId),
    refetchInterval: POLL_MS,
  });
  const { data: squads = [] } = useQuery({
    ...squadListOptions(wsId),
    refetchInterval: POLL_MS,
  });
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);

  // One members roster per department (squad). Squad count is small and
  // workspace-scoped, so N parallel queries here is cheaper than adding a
  // bulk endpoint for a screen this size.
  const squadMembersQueries = useQueries({
    queries: squads.map((squad) => ({
      ...squadMembersOptions(wsId, squad.id),
      refetchInterval: POLL_MS,
    })),
  });

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

  const board = useMemo(
    () => buildBoard(agents, openIssues, doneIssues, presenceMap),
    [agents, openIssues, doneIssues, presenceMap],
  );
  const boardById = useMemo(
    () => new Map(board.map((entry) => [entry.agent.id, entry] as const)),
    [board],
  );

  const idleWithTaskCount = board.filter((entry) => entry.severity >= SEVERITY_WARN).length;

  const departments = useMemo(
    () =>
      squads.map((squad, i) => {
        const squadMembers = squadMembersQueries[i]?.data ?? [];
        const memberIds = Array.from(
          new Set(
            squadMembers
              .filter((m) => m.member_type === "agent")
              .map((m) => m.member_id),
          ),
        );
        // Defensive: a leader is always part of the department even if the
        // membership row is missing, so the org chart never silently drops
        // one.
        if (!memberIds.includes(squad.leader_id)) memberIds.unshift(squad.leader_id);
        return { squad, memberIds };
      }),
    [squads, squadMembersQueries],
  );

  const assignedAgentIds = useMemo(() => {
    const set = new Set<string>();
    for (const dept of departments) {
      for (const id of dept.memberIds) set.add(id);
    }
    return set;
  }, [departments]);

  const unassigned = useMemo(
    () =>
      board
        .filter(
          (entry) =>
            entry.agent.id !== TEAM_LEADER_AGENT_ID && !assignedAgentIds.has(entry.agent.id),
        )
        .sort((a, b) => b.doneCount7d - a.doneCount7d),
    [board, assignedAgentIds],
  );

  const departmentSeverities = useMemo(
    () =>
      new Map(
        departments.map((dept) => [
          dept.squad.id,
          dept.memberIds.reduce<Severity>((max, id) => {
            const s = boardById.get(id)?.severity ?? SEVERITY_OK;
            return s > max ? s : max;
          }, SEVERITY_OK),
        ]),
      ),
    [departments, boardById],
  );

  const teamLeaderEntry = boardById.get(TEAM_LEADER_AGENT_ID) ?? null;
  const teamLeaderSeverity = useMemo(() => {
    let max: Severity = teamLeaderEntry?.severity ?? SEVERITY_OK;
    for (const s of departmentSeverities.values()) {
      if (s > max) max = s;
    }
    return max;
  }, [departmentSeverities, teamLeaderEntry]);

  const owner = members.find((m) => m.role === "owner") ?? null;
  // Rule C ("아이유가 놀면 대표님에게 표시"): fires only when the team lead
  // herself is idle — if she's actively working the chain, there's nothing
  // yet for the owner to act on.
  const ownerAlert =
    teamLeaderSeverity >= SEVERITY_WARN && teamLeaderEntry?.presence?.workload !== "working";

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
      {/* Escalation summary (NEX-1045 "오케스트레이터 존", counts only). */}
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
                        <IssueOwnerChip issue={issue} />
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
          <div className="space-y-4">
            <OrgTopRow owner={owner} teamLeaderEntry={teamLeaderEntry} ownerAlert={ownerAlert} />
            {departments.map((dept) => (
              <DepartmentCard
                key={dept.squad.id}
                squad={dept.squad}
                memberIds={dept.memberIds}
                boardById={boardById}
                severity={departmentSeverities.get(dept.squad.id) ?? SEVERITY_OK}
              />
            ))}
            {unassigned.length > 0 && <UnassignedSection entries={unassigned} />}
          </div>
        </div>
      </div>
    </div>
  );
}

// Reroutes through waiting_on before falling back to assignee — see
// getEffectiveOwnerAgentId. Keeps the sidebar's "who's on this" chip
// consistent with the org chart's grouping instead of showing a stale
// original assignee for an issue the sweeper already moved on.
function IssueOwnerChip({ issue }: { issue: Issue }) {
  const { t } = useT("office");
  const ownerAgentId = getEffectiveOwnerAgentId(issue);
  if (ownerAgentId) {
    return <ActorAvatar actorType="agent" actorId={ownerAgentId} size={14} />;
  }
  if (issue.assignee_type && issue.assignee_id) {
    return <ActorAvatar actorType={issue.assignee_type} actorId={issue.assignee_id} size={14} />;
  }
  return <span>{t(($) => $.page.unassigned)}</span>;
}

// Top-level org-chart row: 대표님(owner) and 아이유(TeamLeader) on the same
// line, per the 2026-08-30 canonical goal ("아이유 위에 내 옆에").
function OrgTopRow({
  owner,
  teamLeaderEntry,
  ownerAlert,
}: {
  owner: { user_id: string; name: string } | null;
  teamLeaderEntry: AgentBoardEntry | null;
  ownerAlert: boolean;
}) {
  const { t } = useT("office");
  if (!owner && !teamLeaderEntry) return null;
  return (
    <section className="flex flex-wrap items-stretch gap-3 rounded-lg border border-border/60 bg-muted/20 p-3">
      {owner && (
        <div
          className={cn(
            "flex items-center gap-3 rounded-lg border p-3",
            ownerAlert ? "border-destructive/60 bg-destructive/5" : "border-transparent",
          )}
        >
          <ActorAvatar actorType="member" actorId={owner.user_id} size={40} />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="truncate text-sm font-medium">{owner.name}</span>
              {ownerAlert && (
                <span className="inline-flex shrink-0 items-center gap-1 rounded bg-destructive/10 px-1.5 py-0.5 text-[10px] text-destructive">
                  <AlertTriangle className="size-3" />
                  {t(($) => $.org.owner_alert_chip)}
                </span>
              )}
            </div>
            <div className="text-[11px] text-muted-foreground">{t(($) => $.org.ceo_label)}</div>
          </div>
        </div>
      )}
      {teamLeaderEntry && (
        <div className="min-w-72 flex-1">
          <AgentOrgRow entry={teamLeaderEntry} roleLabel={t(($) => $.org.team_leader_label)} />
        </div>
      )}
    </section>
  );
}

function DepartmentCard({
  squad,
  memberIds,
  boardById,
  severity,
}: {
  squad: Squad;
  memberIds: string[];
  boardById: Map<string, AgentBoardEntry>;
  severity: Severity;
}) {
  const { t } = useT("office");
  const leaderEntry = boardById.get(squad.leader_id) ?? null;
  const memberEntries = memberIds
    .filter((id) => id !== squad.leader_id)
    .map((id) => boardById.get(id))
    .filter((entry): entry is AgentBoardEntry => !!entry)
    .sort((a, b) => b.doneCount7d - a.doneCount7d);

  return (
    <section
      className={cn(
        "rounded-lg border p-4",
        severity === SEVERITY_WARN && "border-warning/50",
        severity >= SEVERITY_RECALLED && "border-destructive/60",
      )}
    >
      <div className="mb-3 flex items-center gap-2">
        <ActorAvatar actorType="squad" actorId={squad.id} size={24} />
        <h3 className="text-sm font-medium">{squad.name}</h3>
        <span className="text-xs text-muted-foreground">
          {t(($) => $.org.member_count, { count: memberIds.length })}
        </span>
      </div>
      <div className="space-y-2">
        {leaderEntry && <AgentOrgRow entry={leaderEntry} isDepartmentLeader />}
        {memberEntries.map((entry) => (
          <AgentOrgRow key={entry.agent.id} entry={entry} />
        ))}
        {!leaderEntry && memberEntries.length === 0 && (
          <p className="px-1 text-xs text-muted-foreground">{t(($) => $.org.no_members)}</p>
        )}
      </div>
    </section>
  );
}

function UnassignedSection({ entries }: { entries: AgentBoardEntry[] }) {
  const { t } = useT("office");
  return (
    <section className="rounded-lg border border-dashed border-border/60 p-4">
      <div className="mb-1 flex items-center gap-2">
        <h3 className="text-sm font-medium text-muted-foreground">
          {t(($) => $.org.unassigned_heading)}
        </h3>
        <span className="text-xs text-muted-foreground">
          {t(($) => $.org.member_count, { count: entries.length })}
        </span>
      </div>
      <p className="mb-3 text-[11px] text-muted-foreground/70">
        {t(($) => $.org.unassigned_hint)}
      </p>
      <div className="space-y-2">
        {entries.map((entry) => (
          <AgentOrgRow key={entry.agent.id} entry={entry} />
        ))}
      </div>
    </section>
  );
}

function severityRingClass(entry: AgentBoardEntry): string {
  if (entry.severity >= SEVERITY_RECALLED) return "ring-destructive";
  if (entry.severity === SEVERITY_WARN) return "ring-warning";
  return entry.presence?.workload === "working" ? "ring-success" : "ring-border/50";
}

// One bot's row on the org chart: avatar + presence ring, name, throughput /
// load line, and the task list it's actually holding (condition 3 — must be
// the real waiting_on-derived list, not a count badge). Reused for 아이유,
// department leaders, department members, and the unassigned bucket so the
// escalation visuals stay identical everywhere a bot appears.
function AgentOrgRow({
  entry,
  isDepartmentLeader,
  roleLabel,
}: {
  entry: AgentBoardEntry;
  isDepartmentLeader?: boolean;
  roleLabel?: string;
}) {
  const { t } = useT("office");
  const paths = useWorkspacePaths();
  const { agent, presence, held, doneCount7d, severity, reassignHistory, maxWaitHours: agentMaxWait } =
    entry;
  const working = presence?.workload === "working";
  const shown = held.slice(0, HELD_TASKS_SHOWN);
  const extra = held.length - shown.length;

  return (
    <div className="flex items-start gap-3 rounded-lg border border-border/60 p-3">
      <div
        className={cn(
          "relative shrink-0 rounded-full ring-2 ring-offset-2 ring-offset-background",
          severityRingClass(entry),
        )}
      >
        <ActorAvatar
          actorType="agent"
          actorId={agent.id}
          size={40}
          enableHoverCard
          hoverCardVariant="live"
        />
        {severity >= SEVERITY_WARN && (
          <span
            className={cn(
              "absolute -right-0.5 -top-0.5 flex h-4 w-4 items-center justify-center rounded-full text-white",
              severity >= SEVERITY_RECALLED ? "bg-destructive" : "bg-warning",
            )}
          >
            <AlertTriangle className="h-2.5 w-2.5" />
          </span>
        )}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="truncate text-sm font-medium">{agent.name}</span>
          {isDepartmentLeader && (
            <span className="inline-flex shrink-0 items-center gap-0.5 rounded bg-amber-100 px-1.5 py-0.5 text-[10px] text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
              <Crown className="size-3" />
              {t(($) => $.org.leader_chip)}
            </span>
          )}
          {roleLabel && (
            <span className="shrink-0 rounded bg-accent px-1.5 py-0.5 text-[10px] text-accent-foreground">
              {roleLabel}
            </span>
          )}
          {reassignHistory && (
            <span className="shrink-0 rounded bg-destructive/10 px-1.5 py-0.5 text-[10px] text-destructive">
              {t(($) => $.org.reassign_history_chip)}
            </span>
          )}
        </div>
        <div className="mt-0.5 text-[11px] text-muted-foreground">
          {t(($) => $.org.done_7d, { count: doneCount7d })}
          {held.length > 0 ? (
            <> · {t(($) => $.page.load, { count: held.length, wait: formatWaitHours(agentMaxWait) })}</>
          ) : (
            !working && <> · {t(($) => $.page.resting)}</>
          )}
        </div>
        {shown.length > 0 ? (
          <ul className="mt-1.5 space-y-0.5">
            {shown.map((issue) => (
              <li key={issue.id}>
                <AppLink
                  href={paths.issueDetail(issue.id)}
                  className="flex items-center gap-1.5 rounded px-1 py-0.5 text-[11px] transition-colors hover:bg-accent"
                >
                  <StatusIcon status={issue.status} className="h-3 w-3 shrink-0" />
                  <span className="shrink-0 font-mono text-[10px] text-muted-foreground">
                    {issue.identifier}
                  </span>
                  <span className="truncate">{issue.title}</span>
                </AppLink>
              </li>
            ))}
            {extra > 0 && (
              <li className="px-1 text-[11px] text-muted-foreground">
                {t(($) => $.org.held_tasks_more, { count: extra })}
              </li>
            )}
          </ul>
        ) : (
          <p className="mt-1.5 px-1 text-[11px] text-muted-foreground/70">
            {t(($) => $.org.no_held_tasks)}
          </p>
        )}
      </div>
    </div>
  );
}
