package handler

import (
	"context"
	"encoding/json"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTaskUsageReportedAndUnreported(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database required for task usage verification")
	}
	agentID := createHandlerTestAgent(t, "UsageAgent", []byte("[]"))
	var issueID string
	if err := testPool.QueryRow(context.Background(), `INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type) VALUES ($1, 'task-usage-test', 'todo', 'medium', $2, 'member') RETURNING id`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id=$1`, issueID) })
	ids := make([]string, 2)
	for i := range ids {
		if err := testPool.QueryRow(context.Background(), `INSERT INTO agent_task_queue (agent_id, runtime_id, status, issue_id) VALUES ($1, (SELECT runtime_id FROM agent WHERE id=$1), 'completed', $2) RETURNING id`, agentID, issueID).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
		id := ids[i]
		t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id=$1`, id) })
	}
	if _, err := testPool.Exec(context.Background(), `INSERT INTO task_usage(task_id,provider,model,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens) VALUES ($1,'codex','model-a',11,12,13,14),($1,'claude','model-b',21,22,23,24)`, ids[0]); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		w := httptest.NewRecorder()
		testHandler.GetTaskUsage(w, taskUsageRequest(id))
		if w.Code != 200 {
			t.Fatalf("task usage: %d %s", w.Code, w.Body.String())
		}
		var rows []TaskUsageResponse
		if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			if len(rows) != 2 || rows[0].InputTokens != 11 || rows[1].CacheWriteTokens != 24 {
				t.Fatalf("wrong usage: %s", w.Body.String())
			}
		} else if w.Body.String() != "[]\n" {
			t.Fatalf("unreported must be []: %s", w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	testHandler.ListTasksByIssue(w, withURLParam(newRequest("GET", "/api/issues/"+issueID+"/task-runs", nil), "id", issueID))
	if w.Code != 200 {
		t.Fatalf("runs: %d %s", w.Code, w.Body.String())
	}
	var runs []struct {
		ID    string              `json:"id"`
		Usage []TaskUsageResponse `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs: %s", w.Body.String())
	}
	for _, run := range runs {
		if run.Usage == nil {
			t.Fatal("usage must never be null or missing")
		}
		if run.ID == ids[0] && len(run.Usage) != 2 {
			t.Fatal("reported usage missing")
		}
		if run.ID == ids[1] && len(run.Usage) != 0 {
			t.Fatal("unreported usage fabricated")
		}
	}
}

func TestGetTaskUsageCrossWorkspace404(t *testing.T) {
	if testHandler == nil {
		t.Fatal("database required")
	}
	_, id := setupForeignWorkspaceFixture(t)
	w := httptest.NewRecorder()
	testHandler.GetTaskUsage(w, taskUsageRequest(id))
	if w.Code != 404 {
		t.Fatalf("cross workspace: %d %s", w.Code, w.Body.String())
	}
}

func TestGetTaskUsageInvalidID(t *testing.T) {
	w := httptest.NewRecorder()
	testHandler.GetTaskUsage(w, withURLParam(newRequest("GET", "/api/tasks/bad/usage", nil), "taskId", "bad"))
	if w.Code != 400 {
		t.Fatalf("invalid ID: %d", w.Code)
	}
}

func taskUsageRequest(id string) *http.Request {
	req := withURLParam(newRequest("GET", "/api/tasks/"+id+"/usage", nil), "taskId", id)
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, db.Member{}))
}
