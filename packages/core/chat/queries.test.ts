import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { TaskMessagePayload } from "../types/events";
import type { ChatMessagesPage, ChatSession } from "../types";
import {
  chatKeys,
  chatMessagesPageOptions,
  isTaskMessageTaskId,
  mergeTaskMessagesBySeq,
  pickLatestSessionForAgent,
  refreshChatSessionQueries,
  taskMessagesOptions,
} from "./queries";

function installFakeApi(
  listChatMessagesPage: (
    sessionId: string,
    params?: { before?: { created_at: string; id: string } | null; limit?: number },
  ) => Promise<ChatMessagesPage>,
) {
  setApiInstance({ listChatMessagesPage } as unknown as ApiClient);
}

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });
}

const msg = (seq: number): TaskMessagePayload => ({
  task_id: "task-1",
  issue_id: "issue-1",
  seq,
  type: "text",
  content: `m${seq}`,
});

describe("taskMessagesOptions", () => {
  it("fetches task messages for persisted UUID task ids", () => {
    const taskId = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";

    expect(isTaskMessageTaskId(taskId)).toBe(true);
    expect(taskMessagesOptions(taskId).enabled).toBe(true);
  });

  it("does not fetch task messages for optimistic task ids", () => {
    const taskId = "optimistic-optimistic-1778739487737";

    expect(isTaskMessageTaskId(taskId)).toBe(false);
    expect(taskMessagesOptions(taskId).enabled).toBe(false);
  });
});

describe("chatMessagesPageOptions", () => {
  it("defaults the initial page to 5 messages — NEX-881 web chat 5+more", async () => {
    const listChatMessagesPage = vi.fn(async () => ({
      messages: [],
      limit: 5,
      has_more: false,
      next_cursor: null,
    }));
    installFakeApi(listChatMessagesPage);

    const options = chatMessagesPageOptions("session-1");
    await options.queryFn!({
      pageParam: options.initialPageParam,
    } as Parameters<NonNullable<typeof options.queryFn>>[0]);

    expect(listChatMessagesPage).toHaveBeenCalledWith("session-1", {
      before: null,
      limit: 5,
    });
  });

  it("still allows an explicit limit override for follow-up pages", async () => {
    const listChatMessagesPage = vi.fn(async () => ({
      messages: [],
      limit: 20,
      has_more: false,
      next_cursor: null,
    }));
    installFakeApi(listChatMessagesPage);

    const options = chatMessagesPageOptions("session-1", 20);
    await options.queryFn!({
      pageParam: options.initialPageParam,
    } as Parameters<NonNullable<typeof options.queryFn>>[0]);

    expect(listChatMessagesPage).toHaveBeenCalledWith("session-1", {
      before: null,
      limit: 20,
    });
  });
});

describe("refreshChatSessionQueries", () => {
  const sessionId = "session-1";
  const wsId = "ws-1";

  it("re-syncs messages, pending-task status, and the session list — not just the transcript", async () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    await refreshChatSessionQueries(qc, { sessionId, wsId, pendingTaskId: null });

    expect(invalidate).toHaveBeenCalledWith({ queryKey: chatKeys.messagesPage(sessionId) });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: chatKeys.pendingTask(sessionId) });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: chatKeys.sessions(wsId) });
  });

  it("also re-syncs the live task timeline when a real task is pending", async () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    const pendingTaskId = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";

    await refreshChatSessionQueries(qc, { sessionId, wsId, pendingTaskId });

    expect(invalidate).toHaveBeenCalledWith({ queryKey: chatKeys.taskMessages(pendingTaskId) });
  });

  it("skips the task-timeline invalidation for an optimistic (not-yet-persisted) task id", async () => {
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    const optimisticTaskId = "optimistic-optimistic-1778739487737";

    await refreshChatSessionQueries(qc, { sessionId, wsId, pendingTaskId: optimisticTaskId });

    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: chatKeys.taskMessages(optimisticTaskId),
    });
  });
});

describe("mergeTaskMessagesBySeq", () => {
  it("backfills missing seqs and keeps the list seq-ordered", () => {
    const existing = [msg(1), msg(3)];
    const merged = mergeTaskMessagesBySeq(existing, [msg(2), msg(4)]);

    expect(merged.map((m) => m.seq)).toEqual([1, 2, 3, 4]);
  });

  it("drops duplicate seqs and lets the existing entry win", () => {
    const existing = [{ ...msg(1), content: "ws" }];
    const merged = mergeTaskMessagesBySeq(existing, [
      { ...msg(1), content: "refetch" },
      msg(2),
    ]);

    expect(merged.map((m) => m.seq)).toEqual([1, 2]);
    expect(merged.find((m) => m.seq === 1)?.content).toBe("ws");
  });

  it("preserves the array reference when nothing new arrives", () => {
    const existing = [msg(1), msg(2)];

    // Empty incoming and fully-duplicate incoming must both no-op so React
    // Query observers don't re-render on replayed events.
    expect(mergeTaskMessagesBySeq(existing, [])).toBe(existing);
    expect(mergeTaskMessagesBySeq(existing, [msg(1), msg(2)])).toBe(existing);
  });
});

function session(overrides: Partial<ChatSession> & Pick<ChatSession, "id" | "agent_id">): ChatSession {
  return {
    workspace_id: "ws-1",
    creator_id: "user-1",
    title: "",
    status: "active",
    has_unread: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

describe("pickLatestSessionForAgent", () => {
  it("returns the most recently updated session for the given agent", () => {
    const sessions = [
      session({ id: "s-old", agent_id: "agent-1", updated_at: "2026-08-01T00:00:00Z" }),
      session({ id: "s-new", agent_id: "agent-1", updated_at: "2026-08-09T00:00:00Z" }),
      session({ id: "s-other-agent", agent_id: "agent-2", updated_at: "2026-08-10T00:00:00Z" }),
    ];

    expect(pickLatestSessionForAgent(sessions, "agent-1")?.id).toBe("s-new");
  });

  it("skips archived sessions", () => {
    const sessions = [
      session({ id: "s-archived", agent_id: "agent-1", status: "archived", updated_at: "2026-08-09T00:00:00Z" }),
      session({ id: "s-active", agent_id: "agent-1", updated_at: "2026-08-01T00:00:00Z" }),
    ];

    expect(pickLatestSessionForAgent(sessions, "agent-1")?.id).toBe("s-active");
  });

  it("returns null when the agent has no session", () => {
    const sessions = [session({ id: "s-1", agent_id: "agent-2" })];

    expect(pickLatestSessionForAgent(sessions, "agent-1")).toBeNull();
  });
});
