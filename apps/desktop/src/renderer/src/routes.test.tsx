import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { RouterProvider } from "react-router-dom";
import { Outlet } from "react-router-dom";

// NEX-1040 validator FAIL: the shared sidebar (packages/views/layout/
// app-sidebar.tsx, rendered on both web and desktop) links to `/office`,
// but the desktop route tree had no matching entry — so on desktop the
// nav item was a dead link. This test proves the route now exists and
// actually renders OfficePage, not just that the sidebar has a link.
//
// Every other page import in routes.tsx is mocked to a trivial stand-in —
// this test's only concern is route wiring, not those pages' own behavior
// (each has its own tests). WorkspaceRouteLayout is mocked to a plain
// <Outlet/> passthrough so this file doesn't need to reproduce its auth/
// workspace-query mock recipe (see workspace-route-layout.test.tsx for that).
vi.mock("./components/workspace-route-layout", () => ({
  WorkspaceRouteLayout: () => <Outlet />,
}));
vi.mock("./components/route-error-page", () => ({
  DesktopRouteErrorPage: () => <div>route error</div>,
}));
vi.mock("./pages/issue-detail-page", () => ({ IssueDetailPage: () => null }));
vi.mock("./pages/project-detail-page", () => ({ ProjectDetailPage: () => null }));
vi.mock("./pages/autopilot-detail-page", () => ({ AutopilotDetailPage: () => null }));
vi.mock("./pages/skill-detail-page", () => ({ SkillDetailPage: () => null }));
vi.mock("./pages/agent-detail-page", () => ({ AgentDetailPage: () => null }));
vi.mock("./pages/member-detail-page", () => ({ MemberDetailPage: () => null }));
vi.mock("./pages/runtime-detail-page", () => ({ RuntimeDetailPage: () => null }));
vi.mock("./pages/attachment-preview-page", () => ({ AttachmentPreviewRoute: () => null }));
vi.mock("./components/desktop-runtimes-page", () => ({ DesktopRuntimesPage: () => null }));
vi.mock("./components/desktop-agents-page", () => ({ DesktopAgentsPage: () => null }));
vi.mock("./components/daemon-settings-tab", () => ({ DaemonSettingsTab: () => null }));
vi.mock("./components/updates-settings-tab", () => ({ UpdatesSettingsTab: () => null }));
vi.mock("@multica/views/issues/components", () => ({ IssuesPage: () => null }));
vi.mock("@multica/views/office", () => ({
  // The one component this test actually cares about — a marker so we can
  // assert it (not some other route) rendered.
  OfficePage: () => <div data-testid="office-page-marker">office</div>,
}));
vi.mock("@multica/views/projects/components", () => ({ ProjectsPage: () => null }));
vi.mock("@multica/views/dashboard", () => ({ DashboardPage: () => null }));
vi.mock("@multica/views/autopilots/components", () => ({ AutopilotsPage: () => null }));
vi.mock("@multica/views/my-issues", () => ({ MyIssuesPage: () => null }));
vi.mock("@multica/views/skills", () => ({ SkillsPage: () => null }));
vi.mock("@multica/views/squads/components", () => ({
  SquadsPage: () => null,
  SquadDetailPage: () => null,
}));
vi.mock("@multica/views/inbox", () => ({ InboxPage: () => null }));
vi.mock("@multica/views/settings", () => ({ SettingsPage: () => null }));
vi.mock("@multica/views/i18n", () => ({ useT: () => ({ t: (fn: (r: unknown) => string) => fn({}) }) }));

import { appRoutes, createTabRouter } from "./routes";

describe("appRoutes", () => {
  it("registers a workspace-scoped office route", () => {
    const workspaceRoute = appRoutes[0]?.children?.find(
      (route) => route.path === ":workspaceSlug",
    );
    const officeRoute = workspaceRoute?.children?.find((route) => route.path === "office");

    expect(officeRoute).toBeDefined();
    expect(officeRoute?.handle).toEqual({ title: "Office" });
  });

  it("navigating to /acme/office renders OfficePage", async () => {
    const router = createTabRouter("/acme/office");
    render(<RouterProvider router={router} />);

    expect(await screen.findByTestId("office-page-marker")).toBeInTheDocument();
  });
});
