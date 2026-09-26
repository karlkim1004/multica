package handler

import (
	"context"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"net/http/httptest"
	"testing"
)

func TestAutopilotChatDeliveryAuthorizationAndIdempotence(t *testing.T) {
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	q := testHandler.Queries.WithTx(tx)
	var agentID, sessionID, apID, runID, taskID pgtype.UUID
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(tx.QueryRow(ctx, `INSERT INTO agent(workspace_id,name,runtime_mode,visibility,owner_id,runtime_id) VALUES($1,'delivery test','local','private',$2,$3) RETURNING id`, testWorkspaceID, testUserID, testRuntimeID).Scan(&agentID))
	must(tx.QueryRow(ctx, `INSERT INTO chat_session(workspace_id,agent_id,creator_id,title) VALUES($1,$2,$3,'delivery test') RETURNING id`, testWorkspaceID, agentID, testUserID).Scan(&sessionID))
	must(tx.QueryRow(ctx, `INSERT INTO autopilot(workspace_id,title,assignee_type,assignee_id,execution_mode,created_by_type,created_by_id,delivery_chat_session_id) VALUES($1,'delivery','agent',$2,'run_only','member',$3,$4) RETURNING id`, testWorkspaceID, agentID, testUserID, sessionID).Scan(&apID))
	must(tx.QueryRow(ctx, `INSERT INTO autopilot_run(autopilot_id,source,status) VALUES($1,'manual','running') RETURNING id`, apID).Scan(&runID))
	must(tx.QueryRow(ctx, `INSERT INTO agent_task_queue(agent_id,status,autopilot_run_id,runtime_id) VALUES($1,'completed',$2,$3) RETURNING id`, agentID, runID, testRuntimeID).Scan(&taskID))
	_, err = tx.Exec(ctx, `UPDATE autopilot_run SET task_id=$1 WHERE id=$2`, taskID, runID)
	must(err)
	params := db.CanDeliverAutopilotToChatParams{ID: sessionID, WorkspaceID: parseUUID(testWorkspaceID), CreatorID: parseUUID(testUserID), AgentID: agentID}
	allowed, err := q.CanDeliverAutopilotToChat(ctx, params)
	must(err)
	if !allowed {
		t.Fatal("own session rejected")
	}
	bad := params
	bad.CreatorID = agentID
	allowed, err = q.CanDeliverAutopilotToChat(ctx, bad)
	must(err)
	if allowed {
		t.Fatal("foreign creator allowed")
	}
	bad = params
	bad.WorkspaceID = agentID
	allowed, err = q.CanDeliverAutopilotToChat(ctx, bad)
	must(err)
	if allowed {
		t.Fatal("foreign workspace allowed")
	}
	bad = params
	bad.AgentID = sessionID
	allowed, err = q.CanDeliverAutopilotToChat(ctx, bad)
	must(err)
	if allowed {
		t.Fatal("different agent allowed")
	}
	msg, err := q.DeliverAutopilotToChat(ctx, db.DeliverAutopilotToChatParams{RunID: runID, Content: "report"})
	must(err)
	if msg.Role != "assistant" || msg.ChatSessionID != sessionID {
		t.Fatalf("wrong report identity: %+v", msg)
	}
	_, err = q.DeliverAutopilotToChat(ctx, db.DeliverAutopilotToChatParams{RunID: runID, Content: "duplicate"})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("duplicate inserted: %v", err)
	}

	// Exercise the public retry handler: it must preserve the existing report,
	// reject another actor, and never enqueue work or change a running task.
	_, err = tx.Exec(ctx, `UPDATE autopilot_run SET status='completed' WHERE id=$1`, runID)
	must(err)
	_, err = tx.Exec(ctx, `UPDATE agent_task_queue SET result='{"output":"report"}'::jsonb WHERE id=$1`, taskID)
	must(err)
	h := *testHandler
	h.Queries = q
	h.AutopilotService = &service.AutopilotService{Queries: q}
	retry := func(actor string) *httptest.ResponseRecorder {
		req := newRequest("POST", "/api/autopilots/retry", nil)
		req.Header.Set("X-User-ID", actor)
		route := chi.NewRouteContext()
		route.URLParams.Add("id", uuidToString(apID))
		route.URLParams.Add("runId", uuidToString(runID))
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
		w := httptest.NewRecorder()
		h.RetryAutopilotDelivery(w, req)
		return w
	}
	if w := retry(uuidToString(agentID)); w.Code != 403 {
		t.Fatalf("foreign retry: %d %s", w.Code, w.Body.String())
	}
	if w := retry(testUserID); w.Code != 200 {
		t.Fatalf("own retry: %d %s", w.Code, w.Body.String())
	}
	var count int
	must(tx.QueryRow(ctx, `SELECT count(*) FROM chat_message WHERE autopilot_run_id=$1`, runID).Scan(&count))
	if count != 1 {
		t.Fatalf("retry report count=%d", count)
	}
	var delivery string
	must(tx.QueryRow(ctx, `SELECT delivery_status FROM autopilot_run WHERE id=$1`, runID).Scan(&delivery))
	if delivery != "delivered" {
		t.Fatalf("retry status=%s", delivery)
	}
	_, err = tx.Exec(ctx, `UPDATE autopilot_run SET status='running' WHERE id=$1`, runID)
	must(err)
	if w := retry(testUserID); w.Code != 409 {
		t.Fatalf("running retry: %d", w.Code)
	}

	// Remove the first report to prove revocation, rather than deduplication, prevents the next insert.
	_, err = tx.Exec(ctx, `DELETE FROM chat_message WHERE id=$1`, msg.ID)
	must(err)
	_, err = tx.Exec(ctx, `UPDATE member SET role='member' WHERE workspace_id=$1 AND user_id=$2`, testWorkspaceID, testUserID)
	must(err)
	_, err = tx.Exec(ctx, `UPDATE agent SET owner_id=NULL WHERE id=$1`, agentID)
	must(err)
	allowed, err = q.CanDeliverAutopilotToChat(ctx, params)
	must(err)
	if allowed {
		t.Fatal("revoked private access allowed")
	}
	_, err = q.DeliverAutopilotToChat(ctx, db.DeliverAutopilotToChatParams{RunID: runID, Content: "revoked"})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("revoked delivery inserted: %v", err)
	}
	var tasks int
	must(tx.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE chat_session_id=$1`, sessionID).Scan(&tasks))
	if tasks != 0 {
		t.Fatal("delivery created chat task")
	}
}

func TestAutopilotChatDeliveryRejectsInvalidTargets(t *testing.T) {
	for _, tc := range []struct {
		target, mode string
		code         int
	}{
		{"bad-id", "run_only", 400},
		{"00000000-0000-0000-0000-000000000001", "create_issue", 400},
		{"00000000-0000-0000-0000-000000000001", "run_only", 403},
	} {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/autopilots", nil)
		_, ok := testHandler.validateAutopilotChatDelivery(w, req, &tc.target, testUserID, parseUUID(testWorkspaceID), "agent", parseUUID(testRuntimeID), tc.mode)
		if ok || w.Code != tc.code {
			t.Fatalf("target=%s mode=%s: ok=%v code=%d", tc.target, tc.mode, ok, w.Code)
		}
	}
}
