import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";

import type { TaskMessagePayload } from "../types/events";
import {
  chatKeys,
  isTaskMessageTaskId,
  mergeTaskMessagesBySeq,
  refreshChatSessionQueries,
  taskMessagesOptions,
} from "./queries";

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
