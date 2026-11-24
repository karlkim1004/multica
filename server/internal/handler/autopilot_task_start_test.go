package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestStartTask_AutopilotIssueMovesToInProgress(t *testing.T) {
	for _, trigger := range []string{"assignment", "comment", "reassigned"} {
		t.Run(trigger, func(t *testing.T) {
			ctx := context.Background()
			var agentID, autopilotID pgtype.UUID
			if err := testPool.QueryRow(ctx, `
				SELECT id FROM agent WHERE workspace_id = $1 AND name = 'Handler Test Agent'
			`, testWorkspaceID).Scan(&agentID); err != nil {
				t.Fatal(err)
			}
			if err := testPool.QueryRow(ctx, `
				INSERT INTO autopilot (workspace_id, title, assignee_id, execution_mode, created_by_type, created_by_id)
				VALUES ($1, 'start fixture', $2, 'create_issue', 'member', $3)
				RETURNING id
			`, testWorkspaceID, agentID, testUserID).Scan(&autopilotID); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { testPool.Exec(ctx, "DELETE FROM autopilot WHERE id = $1", autopilotID) })

			ap, err := testHandler.Queries.GetAutopilot(ctx, autopilotID)
			if err != nil {
				t.Fatal(err)
			}
			run, err := testHandler.AutopilotService.DispatchAutopilot(ctx, ap, pgtype.UUID{}, "manual", nil)
			if err != nil || !run.IssueID.Valid {
				t.Fatalf("dispatch create_issue: run=%+v err=%v", run, err)
			}
			t.Cleanup(func() { testPool.Exec(ctx, "DELETE FROM issue WHERE id = $1", run.IssueID) })
			before, err := testHandler.Queries.GetIssue(ctx, run.IssueID)
			if err != nil || before.Status != "todo" {
				t.Fatalf("new issue status=%q err=%v", before.Status, err)
			}

			if trigger == "comment" {
				if _, err := testHandler.Queries.CancelAgentTasksByIssue(ctx, before.ID); err != nil {
					t.Fatal(err)
				}
				issueID := uuidToString(before.ID)
				req := newRequest("POST", "/api/issues/"+issueID+"/comments", map[string]string{
					"content": "[@Worker](mention://agent/" + uuidToString(agentID) + ") start assigned work",
				})
				response := httptest.NewRecorder()
				testHandler.CreateComment(response, withURLParam(req, "id", issueID))
				if response.Code != http.StatusCreated {
					t.Fatalf("worker comment: %d %s", response.Code, response.Body)
				}
			}
			if trigger == "reassigned" {
				// Simulate a task already on the way to its old assignee when
				// ownership changes. It must not start the issue.
				if _, err := testPool.Exec(ctx, `
					UPDATE issue SET assignee_id = NULL, assignee_type = NULL WHERE id = $1
				`, before.ID); err != nil {
					t.Fatal(err)
				}
			}
			// Materialize the claim state without starting a real daemon.
			var taskID string
			var commentTriggered bool
			if err := testPool.QueryRow(ctx, `
				UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now()
				WHERE issue_id = $1 AND status = 'queued'
				RETURNING id, trigger_comment_id IS NOT NULL
			`, before.ID).Scan(&taskID, &commentTriggered); err != nil {
				t.Fatal(err)
			}
			if commentTriggered != (trigger == "comment") {
				t.Fatalf("unexpected task trigger: comment=%v", commentTriggered)
			}

			start := func(workspaceID string) *httptest.ResponseRecorder {
				response := httptest.NewRecorder()
				req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/start", nil, workspaceID, "test-daemon")
				testHandler.StartTask(response, withURLParam(req, "taskId", taskID))
				return response
			}
			if response := start("00000000-0000-0000-0000-000000000000"); response.Code != http.StatusNotFound {
				t.Fatalf("foreign start: %d %s", response.Code, response.Body)
			}
			if response := start(testWorkspaceID); response.Code != http.StatusOK {
				t.Fatalf("start: %d %s", response.Code, response.Body)
			}
			after, err := testHandler.Queries.GetIssue(ctx, before.ID)
			if trigger == "reassigned" {
				if err != nil || after.Status != "todo" || after.UpdatedAt != before.UpdatedAt {
					t.Fatalf("reassigned issue status=%q updated_at=%v err=%v", after.Status, after.UpdatedAt, err)
				}
				return
			}
			if err != nil || after.Status != "in_progress" || !after.UpdatedAt.Time.After(before.UpdatedAt.Time) {
				t.Fatalf("start issue status=%q updated_at=%v old=%v err=%v", after.Status, after.UpdatedAt, before.UpdatedAt, err)
			}
			if response := start(testWorkspaceID); response.Code != http.StatusBadRequest {
				t.Fatalf("duplicate start: %d %s", response.Code, response.Body)
			}

			response := httptest.NewRecorder()
			req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete", strings.NewReader("{}"), testWorkspaceID, "test-daemon")
			testHandler.CompleteTask(response, withURLParam(req, "taskId", taskID))
			if response.Code != http.StatusOK {
				t.Fatalf("complete: %d %s", response.Code, response.Body)
			}
			finished, err := testHandler.Queries.GetIssue(ctx, before.ID)
			if err != nil || finished.Status != "in_progress" || finished.UpdatedAt != after.UpdatedAt {
				t.Fatalf("completed task issue status=%q updated_at=%v err=%v", finished.Status, finished.UpdatedAt, err)
			}
		})
	}
}
