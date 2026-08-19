import { randomUUID } from "node:crypto";
import pg from "pg";
import { expect, test } from "@playwright/test";
import { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.E2E_DATABASE_URL ?? process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

type Seed = { runtimeId: string; agentId: string; sessionId: string; workspaceSlug: string };

declare global {
  interface Window {
    __closeE2EWebSocket?: () => void;
    __e2eWebSocketCount?: () => number;
  }
}

async function seedIsolatedChat(api: TestApiClient): Promise<Seed> {
  const { user } = await api.login(
    `e2e-chat-reconnect-${process.env.TEST_PARALLEL_INDEX ?? "0"}-${Date.now()}@multica.ai`,
    "E2E Chat Reconnect User",
  );
  const workspace = await api.ensureWorkspace(
    "E2E Chat Reconnect Workspace",
    `e2e-chat-reconnect-${process.env.TEST_PARALLEL_INDEX ?? "0"}-${Date.now()}`,
  );
  await api.markUserOnboarded();

  const db = new pg.Client(DATABASE_URL);
  await db.connect();
  try {
    const runtime = await db.query<{ id: string }>(
      `INSERT INTO agent_runtime (
         workspace_id, daemon_id, name, runtime_mode, provider, status,
         device_info, metadata, owner_id, last_seen_at
       ) VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, $5, now())
       RETURNING id`,
      [workspace.id, `E2E Reconnect Runtime ${Date.now()}`, "e2e_chat_reconnect", "E2E reconnect runtime", user.id],
    );
    const runtimeId = runtime.rows[0]?.id;
    if (!runtimeId) throw new Error("Failed to seed isolated E2E runtime");

    const agent = await db.query<{ id: string }>(
      `INSERT INTO agent (
         workspace_id, name, description, runtime_mode, runtime_config,
         runtime_id, visibility, max_concurrent_tasks, owner_id
       ) VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [workspace.id, `E2E Reconnect Agent ${Date.now()}`, runtimeId, user.id],
    );
    const agentId = agent.rows[0]?.id;
    if (!agentId) throw new Error("Failed to seed isolated E2E agent");

    // The session is created through the public API. This probe accepts no
    // pre-existing session identifier, so it cannot select or fall back to a
    // representative user's session.
    const session = await api.createChatSession(agentId, "E2E reconnect probe");
    expect(session.http_status).toBe(201);
    expect(session.id).toBeTruthy();
    console.log(JSON.stringify({ workspace: workspace.slug, session: session.id, representative_session_match: false }));
    return { runtimeId, agentId, sessionId: session.id, workspaceSlug: workspace.slug };
  } finally {
    await db.end();
  }
}

test.describe("chat reconnect regression", () => {
  test("refetches the active isolated transcript after a websocket reconnect without refresh", async ({ page }) => {
    const api = new TestApiClient();
    const seed = await seedIsolatedChat(api);
    let taskId: string;
    const reply = `reconnect-reply-${randomUUID()}`;
    const initialMessage = "Reconnect probe initial message";

    // This mock preserves the production WSClient behaviour: close schedules
    // a new socket in 3 seconds, and auth_ack invokes its reconnect callbacks.
    await page.addInitScript(() => {
      class E2EMockWebSocket {
        static OPEN = 1;
        readyState = 0;
        onopen: (() => void) | null = null;
        onmessage: ((event: { data: string }) => void) | null = null;
        onclose: (() => void) | null = null;
        onerror: (() => void) | null = null;
        constructor(..._args: unknown[]) {
          setTimeout(() => {
            this.readyState = E2EMockWebSocket.OPEN;
            this.onopen?.();
          }, 0);
        }
        send(frame: string) {
          if (JSON.parse(frame).type === "auth") {
            this.onmessage?.({ data: JSON.stringify({ type: "auth_ack", payload: {} }) });
          }
        }
        close() {
          this.readyState = 3;
          this.onclose?.();
        }
      }
      const sockets: E2EMockWebSocket[] = [];
      const NativeWebSocket = window.WebSocket;
      window.WebSocket = new Proxy(NativeWebSocket, {
        construct(Target, args) {
          const url = new URL(String(args[0]), location.href);
          // Preserve Next dev's HMR connection; only app realtime uses /ws.
          if (url.pathname !== "/ws") return Reflect.construct(Target, args);
          const socket = new E2EMockWebSocket();
          sockets.push(socket);
          return socket;
        }
      }) as unknown as typeof WebSocket;
      window.__closeE2EWebSocket = () => sockets.at(-1)?.close();
      window.__e2eWebSocketCount = () => sockets.length;
    });
    await page.addInitScript(
      ({ token, sessionId }) => {
        localStorage.setItem("multica_token", token);
        localStorage.setItem("multica:chat:isOpen", "true");
        // The current workspace slug is resolved from the URL by the app.
        localStorage.setItem("multica:chat:activeSessionId", sessionId);
      },
      { token: api.getToken(), sessionId: seed.sessionId },
    );

    const db = new pg.Client(DATABASE_URL);
    await db.connect();
    try {
      const task = await db.query<{ id: string }>(
        `INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, started_at)
         VALUES ($1, $2, $3, 'running', 0, now())
         RETURNING id`,
        [seed.agentId, seed.runtimeId, seed.sessionId],
      );
      taskId = task.rows[0]?.id ?? "";
      expect(taskId).toBeTruthy();
      await db.query(
        `INSERT INTO chat_message (id, chat_session_id, role, content, task_id)
         VALUES ($1, $2, 'user', $3, $4)`,
        [randomUUID(), seed.sessionId, initialMessage, taskId],
      );
    } finally {
      await db.end();
    }

    await page.addInitScript(
      ({ workspaceSlug, sessionId, agentId }) => {
        localStorage.setItem(`multica:chat:activeSessionId:${workspaceSlug}`, sessionId);
        localStorage.setItem(`multica:chat:selectedAgentId:${workspaceSlug}`, agentId);
      },
      { workspaceSlug: seed.workspaceSlug, sessionId: seed.sessionId, agentId: seed.agentId },
    );
    await page.goto(`/${seed.workspaceSlug}/issues`, { waitUntil: "domcontentloaded" });
    await expect(page.getByText(initialMessage)).toBeVisible({ timeout: 15000 });
    await expect.poll(() => page.evaluate(() => window.__e2eWebSocketCount?.() ?? 0)).toBeGreaterThan(0);

    await page.evaluate(() => window.__closeE2EWebSocket?.());
    const disconnectedDb = new pg.Client(DATABASE_URL);
    await disconnectedDb.connect();
    try {
      const completed = await disconnectedDb.query(
        `UPDATE agent_task_queue SET status = 'completed', completed_at = now()
         WHERE id = $1 AND status = 'running'`,
        [taskId],
      );
      expect(completed.rowCount).toBe(1);
      await disconnectedDb.query(
        `INSERT INTO chat_message (id, chat_session_id, role, content, task_id, elapsed_ms)
         VALUES ($1, $2, 'assistant', $3, $4, $5)`,
        [randomUUID(), seed.sessionId, reply, taskId, 1000],
      );
    } finally {
      await disconnectedDb.end();
    }

    // No reload or click occurs after disconnect. A second socket proves the
    // real 3-second WSClient reconnect path ran; the DOM marker proves the
    // messagesPage cache was refetched from the isolated session.
    await expect.poll(() => page.evaluate(() => window.__e2eWebSocketCount?.() ?? 0), { timeout: 10000 }).toBeGreaterThan(1);
    await expect(page.getByText(reply)).toBeVisible({ timeout: 10000 });
    console.log(JSON.stringify({ session: seed.sessionId, task: taskId, lifecycle_completed: true, recovered_without_refresh: true }));

    await api.deleteChatSession(seed.sessionId);
    const cleanupDb = new pg.Client(DATABASE_URL);
    await cleanupDb.connect();
    try {
      await cleanupDb.query("DELETE FROM agent WHERE id = $1", [seed.agentId]);
      await cleanupDb.query("DELETE FROM agent_runtime WHERE id = $1", [seed.runtimeId]);
    } finally {
      await cleanupDb.end();
    }
  });
});
