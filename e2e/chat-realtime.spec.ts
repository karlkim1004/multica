import { randomUUID } from "node:crypto";
import pg from "pg";
import { expect, test } from "@playwright/test";
import { TestApiClient } from "./fixtures";

const DATABASE_URL = process.env.DATABASE_URL ?? "postgres://multica:multica@localhost:5432/multica?sslmode=disable";

type RealtimeEvent = {
  type: "chat:done";
  payload: {
    chat_session_id: string;
    task_id: string;
    message_id: string;
    content: string;
    created_at: string;
    elapsed_ms: number;
  };
};

declare global {
  interface Window {
    __emitRealtimeEvent?: (event: RealtimeEvent) => void;
  }
}

test.describe("chat realtime rendering", () => {
  test("renders a newly completed assistant reply without a manual refresh", async ({ page }) => {
    const api = new TestApiClient();
    const { user } = await api.login(
      `e2e-chat-realtime-${process.env.TEST_PARALLEL_INDEX ?? "0"}-${Date.now()}@multica.ai`,
      "E2E Chat Realtime User",
    );
    const workspace = await api.ensureWorkspace(
      "E2E Chat Realtime Workspace",
      `e2e-chat-realtime-${process.env.TEST_PARALLEL_INDEX ?? "0"}-${Date.now()}`,
    );
    await api.markUserOnboarded();

    const setupDb = new pg.Client(DATABASE_URL);
    await setupDb.connect();
    const runtimeResult = await setupDb.query<{ id: string }>(
      `INSERT INTO agent_runtime (
         workspace_id, daemon_id, name, runtime_mode, provider, status,
         device_info, metadata, owner_id, last_seen_at
       ) VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, $5, now())
       RETURNING id`,
      [
        workspace.id,
        `E2E Realtime Runtime ${Date.now()}`,
        "e2e_chat_realtime",
        "E2E realtime regression runtime",
        user.id,
      ],
    );
    const runtime = runtimeResult.rows[0];
    if (!runtime) throw new Error("Failed to seed isolated E2E realtime runtime");
    const agentResult = await setupDb.query<{ id: string }>(
      `INSERT INTO agent (
         workspace_id, name, description, runtime_mode, runtime_config,
         runtime_id, visibility, max_concurrent_tasks, owner_id
       ) VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
       RETURNING id`,
      [workspace.id, `E2E Realtime Agent ${Date.now()}`, runtime.id, user.id],
    );
    const agent = agentResult.rows[0];
    if (!agent) throw new Error("Failed to seed isolated E2E realtime agent");
    const sessionResult = await setupDb.query<{ id: string }>(
      `INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, status)
       VALUES ($1, $2, $3, 'Realtime rendering regression', 'active')
       RETURNING id`,
      [workspace.id, agent.id, user.id],
    );
    const session = sessionResult.rows[0];
    if (!session) throw new Error("Failed to seed isolated E2E realtime session");
    await setupDb.end();
    const taskId = randomUUID();
    const userMessageId = randomUUID();
    const assistantMessageId = randomUUID();
    const createdAt = new Date().toISOString();
    const reply = `realtime-reply-${randomUUID()}`;
    const db = new pg.Client(DATABASE_URL);
    await db.connect();
    try {
      await db.query(
        `INSERT INTO chat_message (id, chat_session_id, role, content, task_id)
         VALUES ($1, $2, 'user', $3, $4)`,
        [userMessageId, session.id, "Show the realtime reply", taskId],
      );
    } finally {
      await db.end();
    }

    // Keep the browser's realtime transport deterministic while preserving the
    // production protocol: auth_ack is followed by a chat:done frame.
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
          // Next dev's HMR socket is not application realtime. Keeping it
          // native prevents the deterministic chat mock from aborting page
          // navigation before the dashboard mounts.
          if (url.pathname !== "/ws") return Reflect.construct(Target, args);
          const socket = new E2EMockWebSocket();
          sockets.push(socket);
          return socket;
        }
      }) as unknown as typeof WebSocket;
      window.__emitRealtimeEvent = (event) => {
        const frame = { data: JSON.stringify(event) };
        for (const socket of sockets) socket.onmessage?.(frame);
      };
    });
    await page.addInitScript(
      ({ token, workspaceSlug, sessionId, agentId }) => {
        localStorage.setItem("multica_token", token);
        localStorage.setItem("multica:chat:isOpen", "true");
        localStorage.setItem(`multica:chat:activeSessionId:${workspaceSlug}`, sessionId);
        localStorage.setItem(`multica:chat:selectedAgentId:${workspaceSlug}`, agentId);
      },
      {
        token: api.getToken(),
        workspaceSlug: workspace.slug,
        sessionId: session.id,
        agentId: agent.id,
      },
    );
    await page.goto(`/${workspace.slug}/issues`, { waitUntil: "domcontentloaded" });
    await expect(page.getByText("Show the realtime reply")).toBeVisible({ timeout: 15000 });

    const assistantCreatedAt = new Date(Date.now() + 1000).toISOString();
    const db2 = new pg.Client(DATABASE_URL);
    await db2.connect();
    try {
      await db2.query(
        `INSERT INTO chat_message (id, chat_session_id, role, content, task_id, elapsed_ms)
         VALUES ($1, $2, 'assistant', $3, $4, $5)`,
        [assistantMessageId, session.id, reply, taskId, 1000],
      );
    } finally {
      await db2.end();
    }

    if (process.env.E2E_REGRESSION_BREAK !== "1") {
      await page.evaluate(
        (event) => window.__emitRealtimeEvent?.(event),
        {
          type: "chat:done",
          payload: {
            chat_session_id: session.id,
            task_id: taskId,
            message_id: assistantMessageId,
            content: reply,
            created_at: assistantCreatedAt,
            elapsed_ms: 1000,
          },
        } satisfies RealtimeEvent,
      );
    }

    // No reload/click occurs between the event and this assertion: this is the
    // regression gate for the reported manual-refresh symptom.
    await expect(page.getByText(reply)).toBeVisible({ timeout: 10000 });
    await api.deleteChatSession(session.id);
  });
});
