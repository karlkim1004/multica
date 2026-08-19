package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// seedForeignIssue creates an issue owned by the workspace owner (testUserID),
// i.e. one that a super_user must not be able to mutate through any
// issue-derived endpoint.
func seedForeignIssue(t *testing.T, number int, title string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO issue (workspace_id, creator_type, creator_id, title, status, priority, position, number)
		 VALUES ($1,'member',$2,$3,'todo','none',$4,$5) RETURNING id`,
		testWorkspaceID, testUserID, title, number, number).Scan(&id); err != nil {
		t.Fatalf("seed foreign issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id) })
	return id
}

func seedWorkspaceLabel(t *testing.T, name string) string {
	t.Helper()
	var id string
	if err := testPool.QueryRow(context.Background(),
		`INSERT INTO issue_label (workspace_id, name, color) VALUES ($1,$2,'#ffffff') RETURNING id`,
		testWorkspaceID, name).Scan(&id); err != nil {
		t.Fatalf("seed label: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue_label WHERE id = $1`, id) })
	return id
}

// TestSuperUserCannotMutateForeignIssueDerivedRoutes pins the six issue-derived
// mutation routes that previously reached their handler with only
// RequireWorkspaceMember. A super_user may read every issue but may only mutate
// issues it created, so each of these must answer 403 AND leave the row alone.
func TestSuperUserCannotMutateForeignIssueDerivedRoutes(t *testing.T) {
	ctx := context.Background()
	su := createHandlerTestMember(t, RoleSuperUser)

	t.Run("SetIssueMetadataKey", func(t *testing.T) {
		issue := seedForeignIssue(t, 99501, "derived-meta-set")
		w := httptest.NewRecorder()
		testHandler.SetIssueMetadataKey(w, withURLParams(
			newRequestAs(su, "PUT", "/api/issues/"+issue+"/metadata/probe", map[string]any{"value": "x"}),
			"id", issue, "key", "probe"))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		var meta string
		if err := testPool.QueryRow(ctx, `SELECT coalesce(metadata::text,'null') FROM issue WHERE id=$1`, issue).Scan(&meta); err != nil {
			t.Fatal(err)
		}
		if meta != "null" && meta != "{}" {
			t.Fatalf("forbidden write changed metadata: %s", meta)
		}
	})

	t.Run("DeleteIssueMetadataKey", func(t *testing.T) {
		issue := seedForeignIssue(t, 99502, "derived-meta-del")
		if _, err := testPool.Exec(ctx, `UPDATE issue SET metadata = '{"keep":"yes"}'::jsonb WHERE id=$1`, issue); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		testHandler.DeleteIssueMetadataKey(w, withURLParams(
			newRequestAs(su, "DELETE", "/api/issues/"+issue+"/metadata/keep", nil),
			"id", issue, "key", "keep"))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		var meta string
		_ = testPool.QueryRow(ctx, `SELECT metadata::text FROM issue WHERE id=$1`, issue).Scan(&meta)
		if meta != `{"keep": "yes"}` {
			t.Fatalf("forbidden delete changed metadata: %s", meta)
		}
	})

	t.Run("AttachLabel", func(t *testing.T) {
		issue := seedForeignIssue(t, 99503, "derived-label-attach")
		label := seedWorkspaceLabel(t, "rbac-attach")
		w := httptest.NewRecorder()
		testHandler.AttachLabel(w, withURLParam(
			newRequestAs(su, "POST", "/api/issues/"+issue+"/labels", map[string]any{"label_id": label}),
			"id", issue))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		var n int
		_ = testPool.QueryRow(ctx, `SELECT count(*) FROM issue_to_label WHERE issue_id=$1`, issue).Scan(&n)
		if n != 0 {
			t.Fatalf("forbidden attach created %d links", n)
		}
	})

	t.Run("DetachLabel", func(t *testing.T) {
		issue := seedForeignIssue(t, 99504, "derived-label-detach")
		label := seedWorkspaceLabel(t, "rbac-detach")
		if _, err := testPool.Exec(ctx, `INSERT INTO issue_to_label (issue_id, label_id) VALUES ($1,$2)`, issue, label); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		testHandler.DetachLabel(w, withURLParams(
			newRequestAs(su, "DELETE", "/api/issues/"+issue+"/labels/"+label, nil),
			"id", issue, "labelId", label))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		var n int
		_ = testPool.QueryRow(ctx, `SELECT count(*) FROM issue_to_label WHERE issue_id=$1`, issue).Scan(&n)
		if n != 1 {
			t.Fatalf("forbidden detach removed the link: count=%d", n)
		}
	})

	t.Run("RerunIssue", func(t *testing.T) {
		issue := seedForeignIssue(t, 99505, "derived-rerun")
		var before int
		_ = testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issue).Scan(&before)
		w := httptest.NewRecorder()
		testHandler.RerunIssue(w, withURLParam(newRequestAs(su, "POST", "/api/issues/"+issue+"/rerun", nil), "id", issue))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		var after int
		_ = testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, issue).Scan(&after)
		if after != before {
			t.Fatalf("forbidden rerun enqueued a task: %d -> %d", before, after)
		}
	})

	t.Run("CancelTask", func(t *testing.T) {
		issue := seedForeignIssue(t, 99506, "derived-cancel")
		var agentID string
		if err := testPool.QueryRow(ctx,
			`INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id,
			                    visibility, max_concurrent_tasks, owner_id, instructions, custom_env, custom_args)
			 VALUES ($1,'rbac-cancel-agent','','cloud','{}'::jsonb,$2,'workspace',1,$3,'','{}'::jsonb,'[]'::jsonb)
			 RETURNING id`, testWorkspaceID, handlerTestRuntimeID(t), testUserID).Scan(&agentID); err != nil {
			t.Fatalf("seed agent: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id=$1`, agentID) })
		var taskID string
		if err := testPool.QueryRow(ctx,
			`INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
			 VALUES ($1, $2, $3, 'queued', 0) RETURNING id`,
			agentID, handlerTestRuntimeID(t), issue).Scan(&taskID); err != nil {
			t.Fatalf("seed task: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id=$1`, taskID) })

		w := httptest.NewRecorder()
		testHandler.CancelTask(w, withURLParams(
			newRequestAs(su, "POST", "/api/issues/"+issue+"/tasks/"+taskID+"/cancel", nil),
			"id", issue, "taskId", taskID))
		if w.Code != http.StatusForbidden {
			t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
		}
		var status string
		_ = testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id=$1`, taskID).Scan(&status)
		if status != "queued" {
			t.Fatalf("forbidden cancel changed task status: %q", status)
		}
	})
}

// TestSuperUserCanReadForeignIssueDerivedRoutes guards the other direction. The
// matrix gives super_user read access to every issue, so the read siblings of
// the six mutations above must NOT be gated behind the "mutate" decision. Each
// of these regressed once when a mutation guard was inserted one function too
// early.
func TestSuperUserCanReadForeignIssueDerivedRoutes(t *testing.T) {
	su := createHandlerTestMember(t, RoleSuperUser)

	t.Run("GetActiveTaskForIssue", func(t *testing.T) {
		issue := seedForeignIssue(t, 99507, "derived-active-task-read")
		w := httptest.NewRecorder()
		testHandler.GetActiveTaskForIssue(w, withURLParam(
			newRequestAs(su, "GET", "/api/issues/"+issue+"/active-task", nil), "id", issue))
		if w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("ListIssueMetadata", func(t *testing.T) {
		issue := seedForeignIssue(t, 99508, "derived-meta-read")
		w := httptest.NewRecorder()
		testHandler.ListIssueMetadata(w, withURLParam(
			newRequestAs(su, "GET", "/api/issues/"+issue+"/metadata", nil), "id", issue))
		if w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("ListLabelsForIssue", func(t *testing.T) {
		issue := seedForeignIssue(t, 99509, "derived-label-read")
		w := httptest.NewRecorder()
		testHandler.ListLabelsForIssue(w, withURLParam(
			newRequestAs(su, "GET", "/api/issues/"+issue+"/labels", nil), "id", issue))
		if w.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
		}
	})
}
