package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestGetWorkspaceScoreboard covers every bucket the widget renders,
// including the READY-reclassification rule for a stale, idle-assigned
// in_progress issue (NEX-1041): the agent sat idle for over 2 hours after
// its last update, so the issue counts as abandoned work, not work in
// flight.
func TestGetWorkspaceScoreboard(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var workspaceID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, $3, $4)
		RETURNING id
	`, "Scoreboard test", "scoreboard-test-nex-1041", "Scoreboard fixture workspace", "SCB").Scan(&workspaceID); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, workspaceID)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, testUserID); err != nil {
		t.Fatalf("insert member: %v", err)
	}

	var idleAgentID, workingAgentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id, status)
		VALUES ($1, 'scoreboard-idle', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, 'idle')
		RETURNING id
	`, workspaceID, testRuntimeID, testUserID).Scan(&idleAgentID); err != nil {
		t.Fatalf("insert idle agent: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks, owner_id, status)
		VALUES ($1, 'scoreboard-working', '', 'cloud', '{}'::jsonb, $2, 'workspace', 1, $3, 'working')
		RETURNING id
	`, workspaceID, testRuntimeID, testUserID).Scan(&workingAgentID); err != nil {
		t.Fatalf("insert working agent: %v", err)
	}

	// One issue per bucket, plus the stale-idle reclassification case and a
	// fresh in_progress issue on the idle agent that must stay WORKING
	// (updated within the last 2h).
	issues := []struct {
		status       string
		assigneeType string
		assigneeID   string
		age          string // interval expression, "" = now()
	}{
		{"todo", "", "", ""},
		{"in_progress", "agent", workingAgentID, "3 hours"}, // busy agent -> WORKING regardless of age
		{"in_progress", "agent", idleAgentID, "30 minutes"}, // idle but fresh -> WORKING
		{"in_progress", "agent", idleAgentID, "5 hours"},    // idle + stale -> reclassified READY
		{"in_review", "", "", ""},
		{"blocked", "", "", ""},
		{"done", "", "", ""},      // excluded from every bucket
		{"cancelled", "", "", ""}, // excluded from every bucket
	}
	for i, iss := range issues {
		updatedAtExpr := "now()"
		if iss.age != "" {
			updatedAtExpr = "now() - interval '" + iss.age + "'"
		}
		var assigneeTypeVal, assigneeIDVal any
		if iss.assigneeType != "" {
			assigneeTypeVal = iss.assigneeType
			assigneeIDVal = iss.assigneeID
		}
		if _, err := testPool.Exec(ctx, `
			INSERT INTO issue (workspace_id, title, status, priority, assignee_type, assignee_id, creator_type, creator_id, number, updated_at)
			VALUES ($1, $2, $3, 'none', $4, $5, 'member', $6, $7, `+updatedAtExpr+`)
		`, workspaceID, "scoreboard fixture", iss.status, assigneeTypeVal, assigneeIDVal, testUserID, i+1); err != nil {
			t.Fatalf("insert issue %d: %v", i, err)
		}
	}

	req := httptest.NewRequest("GET", "/api/workspace/scoreboard", nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	w := httptest.NewRecorder()

	testHandler.GetWorkspaceScoreboard(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got WorkspaceScoreboard
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}

	// READY = the todo issue + the stale-idle reclassified in_progress issue.
	if got.ReadyCount != 2 {
		t.Errorf("ready_count = %d, want 2 (body=%s)", got.ReadyCount, w.Body.String())
	}
	if got.ReadyMaxWaitHours < 4.9 || got.ReadyMaxWaitHours > 5.1 {
		t.Errorf("ready_max_wait_hours = %v, want ~5.0 (the stale issue's age)", got.ReadyMaxWaitHours)
	}
	// WORKING = busy-agent issue + fresh idle-agent issue.
	if got.WorkingCount != 2 {
		t.Errorf("working_count = %d, want 2", got.WorkingCount)
	}
	if got.VerifyCount != 1 {
		t.Errorf("verify_count = %d, want 1", got.VerifyCount)
	}
	if got.BlockedCount != 1 {
		t.Errorf("blocked_count = %d, want 1", got.BlockedCount)
	}
	if got.BusyAgents != 1 {
		t.Errorf("busy_agents = %d, want 1", got.BusyAgents)
	}
	if got.IdleAgents != 1 {
		t.Errorf("idle_agents = %d, want 1", got.IdleAgents)
	}
	if !got.DispatchFailed {
		t.Errorf("dispatch_failed = false, want true (ready>0 and idle>0)")
	}
}
