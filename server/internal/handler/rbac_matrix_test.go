package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// seedForeignAgent creates a workspace-visible agent owned by the workspace
// owner. Visibility is "workspace" (not "public" — the agent_visibility_check
// constraint only allows workspace|private) so a 403 here comes from member
// RBAC and not from the private-agent gate.
func seedForeignAgent(t *testing.T, name string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
		                    visibility, max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
		 VALUES ($1,$2,'','cloud','{}'::jsonb,$3,'workspace',1,$4,'','{}'::jsonb,'[]'::jsonb) RETURNING id`,
		testWorkspaceID, name, handlerTestRuntimeID(t), testUserID).Scan(&id); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, id) })
	return id
}

// TestSuperUserCannotMutateForeignResources covers the super_user row of the
// RBAC matrix: read every issue, but create/update/delete only what it created.
func TestSuperUserCannotMutateForeignResources(t *testing.T) {
	ctx := context.Background()
	su := createHandlerTestMember(t, RoleSuperUser)

	t.Run("UpdateForeignIssue", func(t *testing.T) {
		issue := seedForeignIssue(t, 99601, "matrix-issue-put")
		w := httptest.NewRecorder()
		testHandler.UpdateIssue(w, withURLParam(
			newRequestAs(su, "PUT", "/api/issues/"+issue, map[string]any{"title": "hijacked"}), "id", issue))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		var title string
		_ = testPool.QueryRow(ctx, `SELECT title FROM issue WHERE id=$1`, issue).Scan(&title)
		if title != "matrix-issue-put" {
			t.Fatalf("forbidden update changed the title: %q", title)
		}
	})

	t.Run("DeleteForeignIssue", func(t *testing.T) {
		issue := seedForeignIssue(t, 99602, "matrix-issue-delete")
		w := httptest.NewRecorder()
		testHandler.DeleteIssue(w, withURLParam(
			newRequestAs(su, "DELETE", "/api/issues/"+issue, nil), "id", issue))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		var n int
		_ = testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE id=$1`, issue).Scan(&n)
		if n != 1 {
			t.Fatalf("forbidden delete removed the row")
		}
	})

	t.Run("DeleteOwnIssueIsAllowed", func(t *testing.T) {
		var own string
		if err := testPool.QueryRow(ctx,
			`INSERT INTO issue (workspace_id, creator_type, creator_id, title, status, priority, position, number)
			 VALUES ($1,'member',$2,'matrix-issue-own','todo','none',99603,99603) RETURNING id`,
			testWorkspaceID, su).Scan(&own); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		testHandler.DeleteIssue(w, withURLParam(newRequestAs(su, "DELETE", "/api/issues/"+own, nil), "id", own))
		if w.Code != http.StatusNoContent {
			t.Fatalf("own delete: want 204, got %d: %s", w.Code, w.Body.String())
		}
		var n int
		_ = testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE id=$1`, own).Scan(&n)
		if n != 0 {
			t.Fatalf("own delete left the row behind")
		}
	})

	t.Run("UpdateForeignAgent", func(t *testing.T) {
		agent := seedForeignAgent(t, "matrix-foreign-agent")
		w := httptest.NewRecorder()
		testHandler.UpdateAgent(w, withURLParams(
			newRequestAs(su, "PUT", "/api/agents/"+agent, map[string]any{"name": "hijacked"}),
			"id", agent, "workspaceId", testWorkspaceID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		var name string
		_ = testPool.QueryRow(ctx, `SELECT name FROM agent WHERE id=$1`, agent).Scan(&name)
		if name != "matrix-foreign-agent" {
			t.Fatalf("forbidden update changed the agent name: %q", name)
		}
	})

	t.Run("UpdateForeignSquad", func(t *testing.T) {
		leader := seedForeignAgent(t, "matrix-squad-leader")
		var squad string
		if err := testPool.QueryRow(ctx,
			`INSERT INTO squad (workspace_id, name, leader_id, creator_id) VALUES ($1,'matrix-foreign-squad',$2,$3) RETURNING id`,
			testWorkspaceID, leader, testUserID).Scan(&squad); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM squad WHERE id=$1`, squad) })
		w := httptest.NewRecorder()
		testHandler.UpdateSquad(w, withURLParams(
			newRequestAs(su, "PUT", "/api/squads/"+squad, map[string]any{"name": "hijacked"}),
			"id", squad, "workspaceId", testWorkspaceID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		var name string
		_ = testPool.QueryRow(ctx, `SELECT name FROM squad WHERE id=$1`, squad).Scan(&name)
		if name != "matrix-foreign-squad" {
			t.Fatalf("forbidden update changed the squad name: %q", name)
		}
	})

	t.Run("ListIssuesIsAllowed", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.ListIssues(w, newRequestAs(su, "GET", "/api/issues/", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("super_user has read over every issue: want 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}

// TestGeneralUserIsIssueCreateOnly covers the general_user row: create issues,
// nothing else.
func TestGeneralUserIsIssueCreateOnly(t *testing.T) {
	ctx := context.Background()
	gu := createHandlerTestMember(t, RoleGeneralUser)

	t.Run("ListAgentsForbidden", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.ListAgents(w, withURLParam(
			newRequestAs(gu, "GET", "/api/agents", nil), "workspaceId", testWorkspaceID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("ListSquadsForbidden", func(t *testing.T) {
		w := httptest.NewRecorder()
		testHandler.ListSquads(w, withURLParam(
			newRequestAs(gu, "GET", "/api/squads", nil), "workspaceId", testWorkspaceID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("UpdateForeignIssueForbidden", func(t *testing.T) {
		issue := seedForeignIssue(t, 99604, "matrix-gu-put")
		w := httptest.NewRecorder()
		testHandler.UpdateIssue(w, withURLParam(
			newRequestAs(gu, "PUT", "/api/issues/"+issue, map[string]any{"title": "hijacked"}), "id", issue))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		var title string
		_ = testPool.QueryRow(ctx, `SELECT title FROM issue WHERE id=$1`, issue).Scan(&title)
		if title != "matrix-gu-put" {
			t.Fatalf("forbidden update changed the title: %q", title)
		}
	})
}

// TestJoinRequestGatesWorkspaceAccessUntilApproved is the completion-condition-3
// end-to-end: a pending applicant is refused, and the same account can create
// issues (and only issues) once an admin approves.
func TestJoinRequestGatesWorkspaceAccessUntilApproved(t *testing.T) {
	ctx := context.Background()

	w := httptest.NewRecorder()
	testHandler.CreateWorkspaceJoinCode(w, withURLParam(
		newRequest("POST", "/api/workspaces/"+testWorkspaceID+"/join-codes", nil), "id", testWorkspaceID))
	if w.Code != http.StatusCreated {
		t.Fatalf("create join code: %d %s", w.Code, w.Body.String())
	}
	var code struct {
		JoinCode string `json:"join_code"`
	}
	_ = json.NewDecoder(w.Body).Decode(&code)

	applicant := createHandlerTestMember(t, RoleGeneralUser)
	if _, err := testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id=$1 AND user_id=$2`, testWorkspaceID, applicant); err != nil {
		t.Fatal(err)
	}

	w = httptest.NewRecorder()
	testHandler.CreateWorkspaceJoinRequest(w, newRequestAs(applicant, "POST", "/api/workspace-join-requests",
		map[string]any{"join_code": code.JoinCode}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create join request: %d %s", w.Code, w.Body.String())
	}
	var request workspaceJoinRequestResponse
	_ = json.NewDecoder(w.Body).Decode(&request)

	// Pending: not a member yet, so workspace APIs must refuse.
	w = httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequestAs(applicant, "POST", "/api/issues", map[string]any{"title": "pending"}))
	if w.Code == http.StatusCreated {
		t.Fatalf("pending applicant created an issue: %d %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.ApproveWorkspaceJoinRequest(w, withURLParams(
		newRequest("POST", "/", nil), "id", testWorkspaceID, "requestId", request.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE creator_id=$1`, applicant) })

	// Approved: general_user may create an issue...
	w = httptest.NewRecorder()
	testHandler.CreateIssue(w, newRequestAs(applicant, "POST", "/api/issues", map[string]any{"title": "approved"}))
	if w.Code != http.StatusCreated {
		t.Fatalf("approved general_user create issue: want 201, got %d: %s", w.Code, w.Body.String())
	}

	// ...and nothing else.
	w = httptest.NewRecorder()
	testHandler.ListAgents(w, withURLParam(newRequestAs(applicant, "GET", "/api/agents", nil), "workspaceId", testWorkspaceID))
	if w.Code != http.StatusForbidden {
		t.Fatalf("approved general_user listing agents: want 403, got %d: %s", w.Code, w.Body.String())
	}
}
