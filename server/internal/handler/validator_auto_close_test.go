package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestValidationAutoCloseReasonsDefaultDenyAndGates(t *testing.T) {
	implementation := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	issue := db.Issue{Status: "in_review", AutoCloseAllowed: true, ImplementationAgentID: implementation, CurrentRef: pgtype.Text{String: "abc", Valid: true}, AutoCloseCriteriaVersion: pgtype.Text{String: "v1", Valid: true}}
	verdict := db.Comment{VerifiedRef: pgtype.Text{String: "abc", Valid: true}, CriteriaVersion: pgtype.Text{String: "v1", Valid: true}}
	if got := validationAutoCloseReasons(issue, verdict, "00000000-0000-0000-0000-000000000002", 0, 0); len(got) != 0 {
		t.Fatalf("safe verdict rejected: %v", got)
	}
	sameVerifier := issue
	sameVerifier.ImplementationAgentID = pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	// [16]byte{2} renders as 02000000-0000-0000-0000-000000000000,
	// not a UUID whose final byte is 2. Use the identical UUID so this
	// asserts the self-verification rejection rather than a different actor.
	if got := validationAutoCloseReasons(sameVerifier, verdict, "02000000-0000-0000-0000-000000000000", 0, 0); !strings.Contains(strings.Join(got, " "), "independent") {
		t.Fatalf("same verifier was not rejected: %v", got)
	}

	cases := []struct {
		name          string
		mutate        func(*db.Issue, *db.Comment)
		prs, children int64
		want          string
	}{
		{"policy", func(i *db.Issue, _ *db.Comment) { i.AutoCloseAllowed = false }, 0, 0, "auto_close_allowed"},
		{"missing implementation", func(i *db.Issue, _ *db.Comment) { i.ImplementationAgentID = pgtype.UUID{} }, 0, 0, "independent"},
		{"stale ref", func(_ *db.Issue, c *db.Comment) { c.VerifiedRef.String = "old" }, 0, 0, "verified_ref"},
		{"stale criteria", func(_ *db.Issue, c *db.Comment) { c.CriteriaVersion.String = "v0" }, 0, 0, "criteria_version"},
		{"external", func(i *db.Issue, _ *db.Comment) { i.ExternalValidationRequired = true }, 0, 0, "external validation"},
		{"open pr", func(_ *db.Issue, _ *db.Comment) {}, 1, 0, "linked PR"},
		{"open child", func(_ *db.Issue, _ *db.Comment) {}, 0, 1, "child issue"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			i, c := issue, verdict
			tc.mutate(&i, &c)
			got := validationAutoCloseReasons(i, c, "different", tc.prs, tc.children)
			if !strings.Contains(strings.Join(got, " "), tc.want) {
				t.Fatalf("%s gate missing: %v", tc.want, got)
			}
		})
	}
}

func TestUpdateIssue_AgentCannotChangeAutoClosePolicy(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	caller := createHandlerTestAgent(t, "auto-close-policy-escalation", nil)
	taskID := insertHandlerTestTask(t, caller)

	create := httptest.NewRecorder()
	createReq := newRequest(http.MethodPost, "/api/issues?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "auto-close policy authorization test",
		"status": "in_review",
	})
	testHandler.CreateIssue(create, createReq)
	if create.Code != http.StatusCreated {
		t.Fatalf("CreateIssue: expected 201, got %d: %s", create.Code, create.Body.String())
	}
	var issue IssueResponse
	if err := json.NewDecoder(create.Body).Decode(&issue); err != nil {
		t.Fatalf("decode issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, "DELETE FROM agent_task_queue WHERE issue_id = $1", issue.ID)
		testPool.Exec(ctx, "DELETE FROM issue WHERE id = $1", issue.ID)
	})

	update := httptest.NewRecorder()
	updateReq := newRequest(http.MethodPut, "/api/issues/"+issue.ID, map[string]any{
		"auto_close_allowed": true,
	})
	updateReq = withURLParam(updateReq, "id", issue.ID)
	updateReq.Header.Set("X-Actor-Source", "task_token")
	updateReq.Header.Set("X-Agent-ID", caller)
	updateReq.Header.Set("X-Task-ID", taskID)
	testHandler.UpdateIssue(update, updateReq)
	if update.Code != http.StatusForbidden {
		t.Fatalf("agent auto-close policy update: expected 403, got %d: %s", update.Code, update.Body.String())
	}

	var enabled bool
	if err := testPool.QueryRow(ctx, "SELECT auto_close_allowed FROM issue WHERE id = $1", issue.ID).Scan(&enabled); err != nil {
		t.Fatalf("read auto-close policy: %v", err)
	}
	if enabled {
		t.Fatal("task-scoped agent enabled auto-close policy")
	}
}
