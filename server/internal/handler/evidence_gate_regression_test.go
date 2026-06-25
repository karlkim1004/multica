package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type evidenceGateRegressionFixture struct {
	IssueKey         string            `json:"issue_key"`
	Title            string            `json:"title"`
	Metadata         map[string]any    `json:"metadata"`
	Runs             []evidenceGateRun `json:"runs"`
	Comments         []string          `json:"comments"`
	WantHTTPStatus   int               `json:"want_http_status"`
	WantStatus       string            `json:"want_status"`
	WantBodyContains []string          `json:"want_body_contains"`
}

type evidenceGateRun struct {
	Status        string          `json:"status"`
	FailureReason string          `json:"failure_reason"`
	Error         string          `json:"error"`
	Result        json.RawMessage `json:"result"`
}

func TestEvidenceGateRegressionFixtures(t *testing.T) {
	for _, fixture := range loadEvidenceGateRegressionFixtures(t) {
		t.Run(fixture.IssueKey, func(t *testing.T) {
			issueID := createTestIssue(t, fixture.Title, "in_progress", "urgent")
			t.Cleanup(func() { deleteTestIssue(t, issueID) })
			seedEvidenceGateRegressionFixture(t, issueID, fixture)

			w := httptest.NewRecorder()
			req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{
				"status": "done",
			})
			req = withURLParam(req, "id", issueID)
			testHandler.UpdateIssue(w, req)

			if w.Code != fixture.WantHTTPStatus {
				t.Fatalf("UpdateIssue done: got HTTP %d, want %d: %s", w.Code, fixture.WantHTTPStatus, w.Body.String())
			}
			for _, want := range fixture.WantBodyContains {
				if !strings.Contains(w.Body.String(), want) {
					t.Fatalf("response body %q does not contain %q", w.Body.String(), want)
				}
			}

			var status string
			if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
				t.Fatalf("load issue status: %v", err)
			}
			if status != fixture.WantStatus {
				t.Fatalf("issue status = %q, want %q", status, fixture.WantStatus)
			}
		})
	}
}

func loadEvidenceGateRegressionFixtures(t *testing.T) []evidenceGateRegressionFixture {
	t.Helper()

	path := filepath.Join("testdata", "evidence_gate_regressions", "nex_587_588_594.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	var fixtures []evidenceGateRegressionFixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatalf("decode fixture %s: %v", path, err)
	}
	if len(fixtures) == 0 {
		t.Fatalf("fixture %s is empty", path)
	}
	return fixtures
}

func seedEvidenceGateRegressionFixture(t *testing.T, issueID string, fixture evidenceGateRegressionFixture) {
	t.Helper()

	if fixture.Metadata != nil {
		rawMetadata, err := json.Marshal(fixture.Metadata)
		if err != nil {
			t.Fatalf("marshal metadata: %v", err)
		}
		if _, err := testPool.Exec(context.Background(), `
			UPDATE issue
			SET metadata = $2::jsonb
			WHERE id = $1
		`, issueID, string(rawMetadata)); err != nil {
			t.Fatalf("seed issue metadata: %v", err)
		}
	}
	for _, comment := range fixture.Comments {
		insertIssueCommentForTest(t, issueID, comment)
	}
	if len(fixture.Runs) == 0 {
		return
	}

	var agentID string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}
	for _, run := range fixture.Runs {
		result := run.Result
		if len(result) == 0 {
			result = json.RawMessage(`{}`)
		}
		if _, err := testPool.Exec(context.Background(), `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, issue_id, status, priority, failure_reason, error, result, completed_at
			)
			VALUES ($1, $2, $3, $4, 0, NULLIF($5, ''), NULLIF($6, ''), $7::jsonb, now())
		`, agentID, handlerTestRuntimeID(t), issueID, run.Status, run.FailureReason, run.Error, string(result)); err != nil {
			t.Fatalf("seed run for %s: %v", fixture.IssueKey, err)
		}
	}
}

func TestEvidenceGateRegressionFixtureContainsRequiredCases(t *testing.T) {
	fixtures := loadEvidenceGateRegressionFixtures(t)
	want := map[string]bool{
		"NEX-587":        false,
		"NEX-588":        false,
		"NEX-594":        false,
		"NEX-602-normal": false,
	}
	for _, fixture := range fixtures {
		if _, ok := want[fixture.IssueKey]; ok {
			want[fixture.IssueKey] = true
		}
	}
	for key, seen := range want {
		if !seen {
			t.Fatalf("missing required regression fixture %s", key)
		}
	}
}
