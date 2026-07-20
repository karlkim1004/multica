package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestSuperUserOwnsAgentEnvAndSquadMutations fixes the public RBAC contract:
// an owning super_user may mutate, while ordinary members cannot alter data.
func TestSuperUserOwnsAgentEnvAndSquadMutations(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "rbac-owned-env-agent", nil)
	if _, err := testPool.Exec(ctx, `UPDATE member SET role = 'super_user' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	})

	envReq := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID+"/env", map[string]any{"custom_env": map[string]string{"RBAC": "ok"}}), "id", agentID)
	envW := httptest.NewRecorder()
	testHandler.UpdateAgentEnv(envW, envReq)
	if envW.Code != http.StatusOK {
		t.Fatalf("owning super_user env update = %d: %s", envW.Code, envW.Body.String())
	}

	var squadID string
	if err := testPool.QueryRow(ctx, `INSERT INTO squad (workspace_id, name, leader_id, creator_id) VALUES ($1, 'rbac-owned-squad', $2, $3) RETURNING id`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID) })
	squadReq := squadRequest(http.MethodPut, squadID, map[string]any{"name": "rbac-updated-squad"})
	squadW := httptest.NewRecorder()
	testHandler.UpdateSquad(squadW, squadReq)
	if squadW.Code != http.StatusOK {
		t.Fatalf("owning super_user squad update = %d: %s", squadW.Code, squadW.Body.String())
	}
}

func TestMemberCannotMutateAgentEnvOrSquad(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID := createHandlerTestAgent(t, "rbac-member-env-agent", nil)
	if _, err := testPool.Exec(ctx, `UPDATE agent SET custom_env = '{"RBAC":"before"}' WHERE id = $1`, agentID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, `UPDATE member SET role = 'member' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	})

	envReq := withURLParam(newRequest(http.MethodPut, "/api/agents/"+agentID+"/env", map[string]any{"custom_env": map[string]string{"RBAC": "after"}}), "id", agentID)
	envW := httptest.NewRecorder()
	testHandler.UpdateAgentEnv(envW, envReq)
	if envW.Code != http.StatusForbidden {
		t.Fatalf("member env update = %d", envW.Code)
	}
	var env string
	if err := testPool.QueryRow(ctx, `SELECT custom_env::text FROM agent WHERE id = $1`, agentID).Scan(&env); err != nil {
		t.Fatal(err)
	}
	if env != `{"RBAC": "before"}` {
		t.Fatalf("member changed env: %s", env)
	}

	var squadID, name string
	if err := testPool.QueryRow(ctx, `INSERT INTO squad (workspace_id, name, leader_id, creator_id) VALUES ($1, 'rbac-member-squad', $2, $3) RETURNING id`, testWorkspaceID, agentID, testUserID).Scan(&squadID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID) })
	squadReq := squadRequest(http.MethodPut, squadID, map[string]any{"name": "forbidden"})
	squadW := httptest.NewRecorder()
	testHandler.UpdateSquad(squadW, squadReq)
	if squadW.Code != http.StatusForbidden {
		t.Fatalf("member squad update = %d", squadW.Code)
	}
	if err := testPool.QueryRow(ctx, `SELECT name FROM squad WHERE id = $1`, squadID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "rbac-member-squad" {
		t.Fatalf("member changed squad: %q", name)
	}
}

func TestSuperUserCannotDeleteForeignSquad(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	leaderID := createHandlerTestAgent(t, "rbac-foreign-squad-leader", nil)
	var squadID string
	if err := testPool.QueryRow(ctx, `INSERT INTO squad (workspace_id, name, leader_id, creator_id) VALUES ($1, 'rbac-foreign-squad', $2, gen_random_uuid()) RETURNING id`, testWorkspaceID, leaderID).Scan(&squadID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM squad WHERE id = $1`, squadID) })
	if _, err := testPool.Exec(ctx, `UPDATE member SET role = 'super_user' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	})

	w := httptest.NewRecorder()
	testHandler.DeleteSquad(w, squadRequest(http.MethodDelete, squadID, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("foreign squad delete = %d", w.Code)
	}
	var archived bool
	if err := testPool.QueryRow(ctx, `SELECT archived_at IS NOT NULL FROM squad WHERE id = $1`, squadID).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	if archived {
		t.Fatal("foreign squad was archived")
	}
}

func squadRequest(method, squadID string, body any) *http.Request {
	req := newRequest(method, "/api/workspaces/"+testWorkspaceID+"/squads/"+squadID, body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("workspaceId", testWorkspaceID)
	rctx.URLParams.Add("id", squadID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}
