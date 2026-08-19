import pg from "pg";
import { expect, test } from "@playwright/test";
import { TestApiClient } from "./fixtures";

const DATABASE_URL =
  process.env.DATABASE_URL ??
  "postgres://multica:multica@localhost:5432/multica?sslmode=disable";
const REPRESENTATIVE_SESSION_ID =
  process.env.REPRESENTATIVE_CHAT_SESSION_ID ??
  "6346dc9b-9894-408b-a241-c4cefd1ecfd1";

type SeededAgent = { runtimeId: string; agentId: string };

async function seedAgent(db: pg.Client, workspaceId: string, ownerId: string): Promise<SeededAgent> {
  const runtime = await db.query<{ id: string }>(
    `INSERT INTO agent_runtime (
       workspace_id, daemon_id, name, runtime_mode, provider, status,
       device_info, metadata, owner_id, last_seen_at
     ) VALUES ($1, NULL, $2, 'cloud', $3, 'online', $4, '{}'::jsonb, $5, now())
     RETURNING id`,
    [
      workspaceId,
      `E2E Chat Isolation Runtime ${Date.now()}`,
      "e2e_chat_isolation",
      "E2E chat isolation runtime",
      ownerId,
    ],
  );
  const runtimeId = runtime.rows[0]?.id;
  if (!runtimeId) throw new Error("Failed to seed isolated E2E runtime");

  const agent = await db.query<{ id: string }>(
    `INSERT INTO agent (
       workspace_id, name, description, runtime_mode, runtime_config,
       runtime_id, visibility, max_concurrent_tasks, owner_id
     ) VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'workspace', 1, $4)
     RETURNING id`,
    [workspaceId, `E2E Chat Isolation Agent ${Date.now()}`, runtimeId, ownerId],
  );
  const agentId = agent.rows[0]?.id;
  if (!agentId) throw new Error("Failed to seed isolated E2E agent");
  return { runtimeId, agentId };
}

test.describe("chat session isolation", () => {
  test("creates a session before sending and never targets the representative session", async () => {
    const api = new TestApiClient();
    const email = `e2e-chat-isolation-${process.env.TEST_PARALLEL_INDEX ?? "0"}-${Date.now()}@multica.ai`;
    const { user } = await api.login(email, "E2E Chat Isolation User");
    const workspace = await api.ensureWorkspace(
      "E2E Chat Isolation Workspace",
      `e2e-chat-isolation-${process.env.TEST_PARALLEL_INDEX ?? "0"}-${Date.now()}`,
    );
    await api.markUserOnboarded();

    const db = new pg.Client(DATABASE_URL);
    await db.connect();
    let seeded: SeededAgent | undefined;
    let sessionId: string | undefined;
    try {
      seeded = await seedAgent(db, workspace.id, user.id);

      // This is the required create-session flow. It goes through the API,
      // rather than selecting an existing or representative session.
      const session = await api.createChatSession(
        seeded.agentId,
        "E2E isolated chat session",
      );
      sessionId = session.id;
      expect(session.http_status).toBe(201);
      expect(session.workspace_id).toBe(workspace.id);
      expect(session.agent_id).toBe(seeded.agentId);
      expect(session.id).not.toBe(REPRESENTATIVE_SESSION_ID);

      // A successful 201 response is the non-503 chat proof. The test is
      // intentionally scoped to the disposable stack's seeded runtime.
      const message = await api.sendChatMessage(session.id, "isolated-chat-probe");
      expect(message.http_status).toBe(201);
      expect(message.message_id).toBeTruthy();
      expect(message.task_id).toBeTruthy();
      expect(message.created_at).toBeTruthy();

      console.log(
        JSON.stringify({
          workspace: workspace.slug,
          session: session.id,
          representative_session_match: session.id === REPRESENTATIVE_SESSION_ID,
          message: message.message_id,
          http_status: message.http_status,
          status: "chat_send_succeeded",
        }),
      );
    } finally {
      if (sessionId) await api.deleteChatSession(sessionId).catch(() => undefined);
      if (seeded) {
        await db.query("DELETE FROM agent WHERE id = $1", [seeded.agentId]);
        await db.query("DELETE FROM agent_runtime WHERE id = $1", [seeded.runtimeId]);
      }
      await db.end();
    }
  });
});
