"use client";

import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { AlertTriangle, Armchair, Crown, Flame, Monitor, Moon } from "lucide-react";
import type { Issue, Squad } from "@multica/core/types";
import { useWorkspacePresenceMap } from "@multica/core/agents";
import {
  officeOpenIssuesOptions,
  officeRecentDoneOptions,
  buildBoard,
  DESK_INTENSITY_THRESHOLD_HOURS,
  getDeskIntensity,
  type AgentBoardEntry,
  type DeskIntensity,
  type Severity,
  SEVERITY_OK,
  SEVERITY_WARN,
  SEVERITY_RECALLED,
  getBlockedReasonText,
  getEffectiveOwnerAgentId,
  getOwnerHeldIssues,
  getTicketShortLabel,
  getWaitReason,
  getWaitStartedAt,
  hoursSince,
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
// NEX-1072 mock7 (2026-08-30 CEO-approved final spec): a room/desk layout
// replaces the earlier org-chart card layout, but stays CSS-only — no
// pixel-art sprites, no game engine (DeskRPG/Phaser was explicitly rejected
// as too heavy). Avatars are the same real ActorAvatar images used
// everywhere else; "desk" and "room" are just semantic containers around
// them.
const POLL_MS = 30_000;
const TASK_LIST_LIMIT = 30;
const HELD_TASKS_SHOWN = 3;

function formatWaitHours(hours: number): string {
  if (hours < 24) return `${Math.round(hours)}h`;
  return `${Math.floor(hours / 24)}d`;
}

// NEX-1072 mock5/mock7: desk ticket chips carry a left-border wait-bucket
// color independent of escalation severity — this is "how stale is this one
// ticket", not "is this agent idle while holding it".
function waitBucketClass(hours: number): string {
  if (hours >= 12) return "border-l-destructive";
  if (hours >= 4) return "border-l-warning";
  return "border-l-success";
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
            new Date(getWaitStartedAt(a)).getTime() -
            new Date(getWaitStartedAt(b)).getTime(),
        ),
    [issues],
  );

  const maxWaitHours = openIssues.length
    ? hoursSince(getWaitStartedAt(openIssues[0]!))
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

  // NEX-1072 mock5/mock7 ("라운지를 따로 뒀습니다"): agents holding zero
  // tickets are a physically separate group from agents holding a ticket
  // while idle (those stay at their desk, whip-marked — see DeskCard).
  // Department/squad leaders always keep a desk in their room, even at zero
  // load, so a room never silently disappears when its lead is unassigned
  // work.
  const leaderIds = useMemo(
    () => new Set(departments.map((dept) => dept.squad.leader_id)),
    [departments],
  );

  const withoutDepartment = useMemo(
    () =>
      board
        .filter(
          (entry) =>
            entry.agent.id !== TEAM_LEADER_AGENT_ID && !assignedAgentIds.has(entry.agent.id),
        )
        .sort((a, b) => b.doneCount7d - a.doneCount7d),
    [board, assignedAgentIds],
  );

  const orphanedWithWork = useMemo(
    () => withoutDepartment.filter((entry) => entry.held.length > 0),
    [withoutDepartment],
  );

  const lounge = useMemo(
    () =>
      board
        .filter(
          (entry) =>
            entry.agent.id !== TEAM_LEADER_AGENT_ID &&
            !leaderIds.has(entry.agent.id) &&
            entry.held.length === 0,
        )
        .sort((a, b) => b.doneCount7d - a.doneCount7d),
    [board, leaderIds],
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
  // Escalation chain: an agent with no department has no department leader
  // between them and 아이유, so their own severity feeds 아이유 directly —
  // same real-signal-only rule as departmentSeverities, just one layer
  // shorter.
  const teamLeaderSeverity = useMemo(() => {
    let max: Severity = teamLeaderEntry?.severity ?? SEVERITY_OK;
    for (const s of departmentSeverities.values()) {
      if (s > max) max = s;
    }
    for (const entry of orphanedWithWork) {
      if (entry.severity > max) max = entry.severity;
    }
    return max;
  }, [departmentSeverities, teamLeaderEntry, orphanedWithWork]);

  const owner = members.find((m) => m.role === "owner") ?? null;
  const ownerHeld = useMemo(
    () => (owner ? getOwnerHeldIssues(openIssues, owner.user_id) : []),
    [openIssues, owner],
  );
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
                            hours: Math.round(hoursSince(getWaitStartedAt(issue))),
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
            <PresidentRoom
              owner={owner}
              ownerHeld={ownerHeld}
              teamLeaderEntry={teamLeaderEntry}
              ownerAlert={ownerAlert}
            />
            <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
              {departments.map((dept) => (
                <DepartmentRoom
                  key={dept.squad.id}
                  squad={dept.squad}
                  memberIds={dept.memberIds}
                  boardById={boardById}
                  severity={departmentSeverities.get(dept.squad.id) ?? SEVERITY_OK}
                />
              ))}
            </div>
            {orphanedWithWork.length > 0 && <OrphanSection entries={orphanedWithWork} />}
            {lounge.length > 0 && <LoungeSection entries={lounge} />}
          </div>
        </div>
      </div>
      <EscalationLegend />
    </div>
  );
}

// NEX-1072 follow-up (2026-09-02, CEO first reaction to the deployed board:
// "챗찍은? 빨간 것만 있는데" — the stages exist but nothing on screen explains
// what they mean). A fixed footer, not part of the scrollable board, so the
// key stays visible without scrolling. Reuses the exact same icon elements
// the desks render (AlertTriangle/Flame/WhipIcon) and the exact threshold
// constants getDeskIntensity uses, so this can never drift into a second,
// wrong copy of "4/12/24".
function EscalationLegend() {
  const { t } = useT("office");
  return (
    <div className="flex flex-wrap items-center gap-4 border-t border-border/60 px-5 py-2 text-[11px] text-muted-foreground">
      <span className="font-medium text-foreground">{t(($) => $.org.legend_heading)}</span>
      <span className="flex items-center gap-1">
        <AlertTriangle className="size-3 text-warning" aria-hidden="true" />
        {t(($) => $.org.legend_warn, { hours: DESK_INTENSITY_THRESHOLD_HOURS.flame1 })}
      </span>
      <span className="flex items-center gap-1">
        <Flame className="size-3 text-warning" aria-hidden="true" />
        {t(($) => $.org.legend_flame1, { hours: DESK_INTENSITY_THRESHOLD_HOURS.flame1 })}
      </span>
      <span className="flex items-center gap-1">
        <Flame className="size-3 text-destructive" aria-hidden="true" />
        <Flame className="-ml-1.5 size-3 text-destructive" aria-hidden="true" />
        {t(($) => $.org.legend_flame2, { hours: DESK_INTENSITY_THRESHOLD_HOURS.flame2 })}
      </span>
      <span className="flex items-center gap-1">
        <WhipIcon className="size-3 text-destructive" />
        {t(($) => $.org.legend_whip, { hours: DESK_INTENSITY_THRESHOLD_HOURS.whip })}
      </span>
    </div>
  );
}

// Reroutes through waiting_on before falling back to assignee — see
// getEffectiveOwnerAgentId. Keeps the sidebar's "who's on this" chip
// consistent with the office board's grouping instead of showing a stale
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

// Top room in the office: 대표님(owner) and 아이유(TeamLeader) share it, per
// the 2026-08-30 canonical goal ("아이유 위에 내 옆에") and mock6's finding
// that the owner is the workspace's single heaviest ticket-holder — the
// president's room gets desks too, not just a name badge.
function PresidentRoom({
  owner,
  ownerHeld,
  teamLeaderEntry,
  ownerAlert,
}: {
  owner: { user_id: string; name: string } | null;
  ownerHeld: Issue[];
  teamLeaderEntry: AgentBoardEntry | null;
  ownerAlert: boolean;
}) {
  const { t } = useT("office");
  if (!owner && !teamLeaderEntry) return null;
  return (
    <section className="rounded-lg border border-border/60 bg-muted/20 p-3">
      <h3 className="mb-2 px-1 text-xs font-medium text-muted-foreground">
        {t(($) => $.org.president_room_heading)}
      </h3>
      <div className="flex flex-wrap items-stretch gap-3">
        {owner && (
          <div
            className={cn(
              "min-w-72 flex-1 rounded-lg border p-3",
              ownerAlert ? "border-destructive/60 bg-destructive/5" : "border-border/60",
            )}
          >
            <div className="flex items-start gap-3">
              <ActorAvatar actorType="member" actorId={owner.user_id} size={40} />
              <div className="min-w-0 flex-1">
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
                {ownerHeld.length > 0 && (
                  <div className="mt-0.5 text-[11px] text-muted-foreground">
                    {t(($) => $.page.load, {
                      count: ownerHeld.length,
                      wait: formatWaitHours(
                        ownerHeld.reduce((max, issue) => Math.max(max, hoursSince(getWaitStartedAt(issue))), 0),
                      ),
                    })}
                  </div>
                )}
              </div>
            </div>
            <TicketTray issues={ownerHeld} />
          </div>
        )}
        {teamLeaderEntry && (
          <div className="min-w-72 flex-1">
            <DeskCard entry={teamLeaderEntry} roleLabel={t(($) => $.org.team_leader_label)} />
          </div>
        )}
      </div>
    </section>
  );
}

function DepartmentRoom({
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
  // NEX-1072 mock5/mock7: only agents currently holding a ticket keep a desk
  // in the department room — zero-load non-leader members move to the
  // Lounge so an idle-with-work bot (whip target) is never visually mixed
  // in with a bot that simply has nothing assigned.
  const memberEntries = memberIds
    .filter((id) => id !== squad.leader_id)
    .map((id) => boardById.get(id))
    .filter((entry): entry is AgentBoardEntry => !!entry && entry.held.length > 0)
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
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        {leaderEntry && <DeskCard entry={leaderEntry} isDepartmentLeader />}
        {memberEntries.map((entry) => (
          <DeskCard key={entry.agent.id} entry={entry} />
        ))}
        {!leaderEntry && memberEntries.length === 0 && (
          <p className="px-1 text-xs text-muted-foreground">{t(($) => $.org.no_members)}</p>
        )}
      </div>
    </section>
  );
}

// Agents with a ticket but no department at all — a structural gap (no
// squad membership), distinct from the Lounge's "no ticket" gap below.
function OrphanSection({ entries }: { entries: AgentBoardEntry[] }) {
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
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        {entries.map((entry) => (
          <DeskCard key={entry.agent.id} entry={entry} />
        ))}
      </div>
    </section>
  );
}

// NEX-1072 mock5 ("라운지를 따로 뒀습니다"): agents holding zero tickets,
// physically separated from idle-with-work desks so the whip target is
// never ambiguous. Compact avatar-row instead of full desks — there is no
// ticket tray or wait state to show here.
function LoungeSection({ entries }: { entries: AgentBoardEntry[] }) {
  const { t } = useT("office");
  return (
    <section className="rounded-lg border border-border/60 bg-muted/10 p-4">
      <div className="mb-1 flex items-center gap-2">
        <Armchair className="size-4 text-muted-foreground" aria-hidden="true" />
        <h3 className="text-sm font-medium text-muted-foreground">
          {t(($) => $.org.lounge_heading)}
        </h3>
        <span className="text-xs text-muted-foreground">
          {t(($) => $.org.member_count, { count: entries.length })}
        </span>
      </div>
      <p className="mb-3 text-[11px] text-muted-foreground/70">{t(($) => $.org.lounge_hint)}</p>
      <div className="flex flex-wrap gap-3">
        {entries.map((entry) => (
          <div key={entry.agent.id} className="flex items-center gap-1.5">
            <ActorAvatar
              actorType="agent"
              actorId={entry.agent.id}
              size={28}
              enableHoverCard
              hoverCardVariant="live"
            />
            <span className="max-w-20 truncate text-xs text-muted-foreground">
              {entry.agent.name}
            </span>
          </div>
        ))}
      </div>
    </section>
  );
}

function severityRingClass(entry: AgentBoardEntry, intensity: DeskIntensity): string {
  if (entry.severity >= SEVERITY_RECALLED) return "ring-destructive";
  if (entry.severity === SEVERITY_WARN) return intensity >= 2 ? "ring-destructive" : "ring-warning";
  return entry.presence?.workload === "working" ? "ring-success" : "ring-border/50";
}

// NEX-1072 follow-up (2026-09-02, CEO directive after 김채원's 11-case
// misread): a ticket sitting idle in someone's tray reads as "holding it and
// doing nothing" whether it's mid-implementation, queued for someone else's
// review, or stuck on an external blocker — three different situations for
// whoever is deciding whether to escalate. This dot is the compact signal
// (ticket chips are single-line, 11px); the full label lives in the title
// tooltip so it doesn't compete for the chip's limited width.
function WaitReasonDot({ issue }: { issue: Issue }) {
  const { t } = useT("office");
  const reason = getWaitReason(issue);
  const blockedReason = reason === "blocked" ? getBlockedReasonText(issue) : null;
  const label =
    reason === "verifying"
      ? t(($) => $.org.wait_reason_verifying)
      : reason === "blocked"
        ? blockedReason
          ? t(($) => $.org.wait_reason_blocked_with_text, { reason: blockedReason })
          : t(($) => $.org.wait_reason_blocked)
        : t(($) => $.org.wait_reason_implementing);
  return (
    <span
      className={cn(
        "inline-block h-1.5 w-1.5 shrink-0 rounded-full",
        reason === "blocked" && "bg-destructive",
        reason === "verifying" && "bg-info",
        reason === "implementing" && "bg-muted-foreground/40",
      )}
      title={label}
    >
      <span className="sr-only">{label}</span>
    </span>
  );
}

// Ticket chips for a set of held issues — shared by desks and the
// president's room. NEX-1072 mock7: the desk view shows a human-readable
// one-line label (getTicketShortLabel), never the raw issue identifier; the
// full identifier + title are still one click away via the link, and in the
// `title` attribute for hover/assistive-tech users.
function TicketTray({ issues }: { issues: Issue[] }) {
  const { t } = useT("office");
  const paths = useWorkspacePaths();
  const shown = issues.slice(0, HELD_TASKS_SHOWN);
  const extra = issues.length - shown.length;
  if (shown.length === 0) {
    return (
      <p className="mt-1.5 px-1 text-[11px] text-muted-foreground/70">
        {t(($) => $.org.no_held_tasks)}
      </p>
    );
  }
  return (
    <ul className="mt-1.5 space-y-1">
      {shown.map((issue) => (
        <li key={issue.id}>
          <AppLink
            href={paths.issueDetail(issue.id)}
            title={`${issue.identifier} · ${issue.title}`}
            className={cn(
              "flex items-center gap-1 truncate rounded border-l-2 bg-background/60 px-1.5 py-0.5 text-[11px] transition-colors hover:bg-accent",
              waitBucketClass(hoursSince(getWaitStartedAt(issue))),
            )}
          >
            <WaitReasonDot issue={issue} />
            <span className="truncate">{getTicketShortLabel(issue)}</span>
          </AppLink>
        </li>
      ))}
      {extra > 0 && (
        <li className="px-1 text-[11px] text-muted-foreground">
          {t(($) => $.org.held_tasks_more, { count: extra })}
        </li>
      )}
    </ul>
  );
}

// One bot's desk: avatar + escalation ring, a monitor indicator for
// "actively working right now" (independent of escalation state — a bot can
// be mid-ticket and still idle between turns), name, throughput line, and
// the ticket tray it's actually holding (condition 3 — must be the real
// waiting_on-derived list, not a count badge). Reused for 아이유, department
// leaders, department members, and the no-department bucket so the
// escalation visuals stay identical everywhere a bot appears.
function DeskCard({
  entry,
  isDepartmentLeader,
  roleLabel,
}: {
  entry: AgentBoardEntry;
  isDepartmentLeader?: boolean;
  roleLabel?: string;
}) {
  const { t } = useT("office");
  const { agent, presence, held, doneCount7d, severity, reassignHistory, maxWaitHours: agentMaxWait } =
    entry;
  const working = presence?.workload === "working";
  // NEX-1072 states_full spec: flame/shake/aura/whip intensity, desk-local
  // only (see getDeskIntensity doc comment) — never fed into the
  // department/team-lead/owner escalation chain above.
  const intensity = getDeskIntensity(severity >= SEVERITY_WARN, agentMaxWait);

  return (
    <div className="flex items-start gap-3 rounded-lg border border-border/60 p-3">
      <div
        className={cn(
          "relative shrink-0 rounded-full ring-2 ring-offset-2 ring-offset-background",
          severityRingClass(entry, intensity),
          intensity >= 2 && "animate-office-shake animate-office-aura",
        )}
      >
        <ActorAvatar
          actorType="agent"
          actorId={agent.id}
          size={40}
          enableHoverCard
          hoverCardVariant="live"
        />
        {working && <SpeedLines />}
        {severity >= SEVERITY_WARN && intensity === 0 && (
          <span
            className="absolute -right-0.5 -top-0.5 flex h-4 w-4 items-center justify-center rounded-full bg-warning text-white"
            title={t(($) => $.org.idle_with_work_label)}
          >
            <AlertTriangle className="h-2.5 w-2.5" aria-hidden="true" />
            <span className="sr-only">{t(($) => $.org.idle_with_work_label)}</span>
          </span>
        )}
        {intensity >= 1 && (
          <span
            className={cn(
              "absolute -right-1 -top-1 flex items-center rounded-full px-1 py-0.5 text-white",
              intensity >= 2 ? "bg-destructive" : "bg-warning",
            )}
            title={t(($) => $.org.neglected_hours_label, {
              hours: Math.round(agentMaxWait),
              stage: intensity,
            })}
          >
            {Array.from({ length: intensity }).map((_, i) => (
              <Flame key={i} className="h-2.5 w-2.5" aria-hidden="true" />
            ))}
            <span className="sr-only">
              {t(($) => $.org.neglected_hours_label, {
                hours: Math.round(agentMaxWait),
                stage: intensity,
              })}
            </span>
          </span>
        )}
        {intensity >= 3 && (
          <WhipBadge
            label={`${t(($) => $.org.neglected_hours_label, {
              hours: Math.round(agentMaxWait),
              stage: intensity,
            })} · ${t(($) => $.org.whip_label)}`}
          />
        )}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="truncate text-sm font-medium">{agent.name}</span>
          <Monitor
            className={cn(
              "size-3 shrink-0",
              working ? "text-success" : "text-muted-foreground/40",
            )}
            aria-hidden="true"
          />
          {!working && <Moon className="size-3 shrink-0 text-muted-foreground/40" aria-hidden="true" />}
          <span className="sr-only">
            {working ? t(($) => $.org.monitor_working) : t(($) => $.org.monitor_idle)}
          </span>
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
          {held.length > 0 && (
            <> · {t(($) => $.page.load, { count: held.length, wait: formatWaitHours(agentMaxWait) })}</>
          )}
        </div>
        <TicketTray issues={held} />
      </div>
    </div>
  );
}

// NEX-1072 states_full spec, "열일 중" state: three short lines trailing the
// avatar's left edge, each staggered so they read as continuous motion
// rather than a single blink. Decorative only — the accessible label lives
// on the Monitor icon's sr-only text next to it, so this has no aria role.
function SpeedLines() {
  return (
    <span
      className="absolute -left-1.5 top-1/2 flex -translate-y-1/2 flex-col gap-0.5"
      aria-hidden="true"
    >
      {[0, 0.15, 0.3].map((delay) => (
        <span
          key={delay}
          className="h-0.5 w-2 rounded-full bg-success animate-office-speed-line"
          style={{ animationDelay: `${delay}s` }}
        />
      ))}
    </span>
  );
}

// The raw whip mark, factored out of WhipBadge so the escalation legend can
// render the exact same icon (no positioning/animation) next to its "24h+"
// entry — NEX-1072 follow-up requirement that the legend show "채찍 실물",
// not a stand-in emoji.
function WhipIcon({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 16 16" className={className} aria-hidden="true">
      <path
        d="M3 2 Q10 2 8 7 T4 12 T9 14"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.4"
        strokeLinecap="round"
      />
    </svg>
  );
}

// NEX-1072 states_full spec, top escalation tier (24h+ idle-with-work): the
// one visual explicitly reserved for the worst case ("화면에 하나만 떠서
// 시선을 끈다") — plain SVG + CSS swing, no new dependency.
function WhipBadge({ label }: { label: string }) {
  return (
    <span
      className="absolute -left-2 -top-1 flex h-4 w-4 items-center justify-center text-destructive animate-office-whip"
      title={label}
    >
      <WhipIcon className="h-4 w-4" />
      <span className="sr-only">{label}</span>
    </span>
  );
}
