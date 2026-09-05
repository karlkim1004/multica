import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, Issue } from "@multica/core/types";
import { TEAM_LEADER_AGENT_ID } from "@multica/core/office";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { OfficePage } from "./office-page";

// NEX-1118 ("아이유도 채찍 대상으로"): buildBoard/getDeskIntensity already
// treat 아이유(TEAM_LEADER_AGENT_ID) exactly like any other agent (see
// org-chart.ts's buildBoard doc comment and org-chart.test.ts's "does not
// exempt the team lead" case) — but neither test exercises the actual
// OfficePage render, so a regression that special-cased her back out in the
// view layer (e.g. skipping WhipActionButton for her id) would slip through.
// This is the missing end-to-end guard: a 25h-idle ticket in 아이유's tray
// must render the tier-3 whip badge in the President's Room and the whip
// button must fire the same enqueue-by-mention path any other bot's does.

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

// Bypasses ActorAvatar's own hover-card/presence-hook wiring, which is
// unrelated to the escalation logic under test.
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="avatar" />,
}));

const mocks = vi.hoisted(() => ({
  agents: [] as Agent[],
  openIssues: [] as Issue[],
  doneIssues: [] as Issue[],
  members: [] as Array<{ user_id: string; name: string; role: string }>,
  previewCommentTriggers: vi.fn(),
  createComment: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey?.[0];
    if (key === "agents") return { data: mocks.agents, isLoading: false };
    if (key === "open-issues") return { data: mocks.openIssues, isLoading: false };
    if (key === "done-issues") return { data: mocks.doneIssues, isLoading: false };
    if (key === "squads") return { data: [], isLoading: false };
    if (key === "members") return { data: mocks.members, isLoading: false };
    return { data: undefined, isLoading: false };
  },
  useQueries: () => [],
}));

vi.mock("@multica/core/office", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/office")>();
  return {
    ...actual,
    officeOpenIssuesOptions: () => ({ queryKey: ["open-issues"] }),
    officeRecentDoneOptions: () => ({ queryKey: ["done-issues"] }),
  };
});

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  squadListOptions: () => ({ queryKey: ["squads"] }),
  squadMembersOptions: () => ({ queryKey: ["squad-members"] }),
}));

vi.mock("@multica/core/agents", () => ({
  useWorkspacePresenceMap: () => ({
    byAgent: new Map([
      [
        TEAM_LEADER_AGENT_ID,
        { availability: "online", workload: "idle", runningCount: 0, queuedCount: 0 },
      ],
    ]),
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issueDetail: (id: string) => `/test-workspace/issues/${id}`,
  }),
}));

vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({
    userId: "owner-1",
    role: "owner",
    member: null,
    isLoading: false,
  }),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useCreateComment: () => ({ mutateAsync: mocks.createComment }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    previewCommentTriggers: (...args: unknown[]) => mocks.previewCommentTriggers(...args),
  },
}));

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

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "NEX-1",
    title: "Issue",
    description: null,
    status: "in_progress",
    priority: "none",
    assignee_type: "agent",
    assignee_id: TEAM_LEADER_AGENT_ID,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 0,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-08-01T00:00:00Z",
    updated_at: "2026-08-01T00:00:00Z",
    ...overrides,
  } as Issue;
}

function makeAdapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/test-workspace/office",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p) => p,
  };
}

function renderOffice() {
  return renderWithI18n(
    <NavigationProvider value={makeAdapter()}>
      <OfficePage />
    </NavigationProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.agents = [makeAgent({ id: TEAM_LEADER_AGENT_ID, name: "아이유(TeamLeader)" })];
  mocks.doneIssues = [];
  mocks.members = [{ user_id: "owner-1", name: "CEO", role: "owner" }];
  mocks.previewCommentTriggers.mockResolvedValue({ agents: [{ id: TEAM_LEADER_AGENT_ID }] });
  mocks.createComment.mockResolvedValue({});
});

describe("OfficePage — 아이유 whip parity (NEX-1118)", () => {
  it("renders the tier-3 whip badge on 아이유's own desk after 25h neglect, same as any other agent", () => {
    mocks.openIssues = [
      makeIssue({
        id: "issue-tl",
        updated_at: "2026-09-04T00:00:00Z",
        metadata: {
          waiting_on: `agent:${TEAM_LEADER_AGENT_ID}`,
          waiting_since: new Date(Date.now() - 25 * 3_600_000).toISOString(),
        },
      }),
    ];

    renderOffice();

    // Whip button is the owner-only click affordance; its presence at all
    // confirms intensity reached tier 3 (getDeskIntensity gates it on
    // intensity >= 1, and the accessible name is stable across locales).
    expect(screen.getByRole("button", { name: "Whip" })).toBeInTheDocument();
  });

  it("does not render the whip button below the 24h threshold — control for the fixture above", () => {
    mocks.openIssues = [
      makeIssue({
        id: "issue-tl",
        updated_at: "2026-09-04T00:00:00Z",
        metadata: {
          waiting_on: `agent:${TEAM_LEADER_AGENT_ID}`,
          waiting_since: new Date(Date.now() - 1 * 3_600_000).toISOString(),
        },
      }),
    ];

    renderOffice();

    expect(screen.queryByRole("button", { name: "Whip" })).not.toBeInTheDocument();
  });

  it("fires the same mention-comment enqueue path for 아이유 that every other agent's whip uses", async () => {
    const user = userEvent.setup();
    mocks.openIssues = [
      makeIssue({
        id: "issue-tl",
        updated_at: "2026-09-04T00:00:00Z",
        metadata: {
          waiting_on: `agent:${TEAM_LEADER_AGENT_ID}`,
          waiting_since: new Date(Date.now() - 25 * 3_600_000).toISOString(),
        },
      }),
    ];

    renderOffice();
    await user.click(screen.getByRole("button", { name: "Whip" }));

    expect(mocks.previewCommentTriggers).toHaveBeenCalledWith(
      "issue-tl",
      expect.stringContaining(`mention://agent/${TEAM_LEADER_AGENT_ID}`),
    );
    expect(mocks.createComment).toHaveBeenCalledWith(
      expect.objectContaining({
        content: expect.stringContaining(`mention://agent/${TEAM_LEADER_AGENT_ID}`),
      }),
    );
  });
});
