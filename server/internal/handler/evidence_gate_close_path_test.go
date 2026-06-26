package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// insertTaskRunForTest inserts a run-history row for an issue so the Evidence Gate
// case-1 path can be exercised end-to-end through UpdateIssue.
func insertTaskRunForTest(t *testing.T, issueID, status string, createdAt time.Time, failureReason string) string {
	t.Helper()
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var failure any
	if failureReason != "" {
		failure = failureReason
	}
	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, failure_reason, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id
	`, agentID, issueID, runtimeID, status, failure, createdAt).Scan(&taskID); err != nil {
		t.Fatalf("insert task run: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
	return taskID
}

const completeQAPassComment = `
## QA PASS

변경 대상: commit handler-test-evidence
실행 테스트: go test ./internal/handler -run TestEvidenceGate -count=1 PASS
사용자 시나리오: done 전환 경로 PASS
라이브 증거: HTTP 409 차단 응답 본문과 DB status 확인
실패 run scan: failed/cancelled/api_invalid_request/unsupported model 없음
결론: PASS
`

func attemptDoneTransition(t *testing.T, issueID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{"status": "done"})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	return w
}

func assertIssueStatus(t *testing.T, issueID, want string) {
	t.Helper()
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	if status != want {
		t.Fatalf("issue status = %q, want %q", status, want)
	}
}

// NEX-605 pattern: only benign cancelled runs (no failed) + an accepted final PASS
// comment must open the close path so done is allowed.
func TestUpdateIssueDoneAllowsBenignCancelledWithFinalPass(t *testing.T) {
	issueID := createTestIssue(t, "Evidence gate benign cancelled close path", "in_progress", "urgent")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	markIssueDoneEvidenceForTest(t, issueID)

	base := time.Now().Add(-30 * time.Minute)
	// cancelled is the latest run (no completed run after it) — unrecovered on its own.
	insertTaskRunForTest(t, issueID, "completed", base, "")
	insertTaskRunForTest(t, issueID, "cancelled", base.Add(5*time.Minute), "")
	insertIssueCommentForTest(t, issueID, completeQAPassComment)

	w := attemptDoneTransition(t, issueID)
	if w.Code != http.StatusOK {
		t.Fatalf("benign cancelled + accepted PASS: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertIssueStatus(t, issueID, "done")
}

// NEX-607 pattern: a genuine failed run must keep blocking even with an accepted
// final PASS comment (F3 — no regression of true-positive blocking).
func TestUpdateIssueDoneBlocksGenuineFailedRunDespiteFinalPass(t *testing.T) {
	issueID := createTestIssue(t, "Evidence gate genuine failed run", "in_progress", "urgent")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	markIssueDoneEvidenceForTest(t, issueID)

	base := time.Now().Add(-30 * time.Minute)
	insertTaskRunForTest(t, issueID, "failed", base, "agent_error")
	insertIssueCommentForTest(t, issueID, completeQAPassComment)

	w := attemptDoneTransition(t, issueID)
	if w.Code != http.StatusConflict {
		t.Fatalf("genuine failed run: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	assertIssueStatus(t, issueID, "in_progress")
}

// Refinement #1: a cancelled run recovered by a later completed run is benign and
// passes even without any final PASS comment.
func TestUpdateIssueDoneAllowsRecoveredCancelledWithoutPass(t *testing.T) {
	issueID := createTestIssue(t, "Evidence gate recovered cancelled", "in_progress", "urgent")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	markIssueDoneEvidenceForTest(t, issueID)

	base := time.Now().Add(-30 * time.Minute)
	insertTaskRunForTest(t, issueID, "cancelled", base, "")
	insertTaskRunForTest(t, issueID, "completed", base.Add(5*time.Minute), "")

	w := attemptDoneTransition(t, issueID)
	if w.Code != http.StatusOK {
		t.Fatalf("recovered cancelled (completed run after): expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertIssueStatus(t, issueID, "done")
}

// An unrecovered cancelled run (no completed run after it, no final PASS) must still
// block — distinguishes a stuck/abandoned cancel from a benign re-invocation.
func TestUpdateIssueDoneBlocksUnrecoveredCancelledWithoutPass(t *testing.T) {
	issueID := createTestIssue(t, "Evidence gate unrecovered cancelled", "in_progress", "urgent")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	markIssueDoneEvidenceForTest(t, issueID)

	base := time.Now().Add(-30 * time.Minute)
	insertTaskRunForTest(t, issueID, "completed", base, "")
	insertTaskRunForTest(t, issueID, "cancelled", base.Add(5*time.Minute), "")

	w := attemptDoneTransition(t, issueID)
	if w.Code != http.StatusConflict {
		t.Fatalf("unrecovered cancelled: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	assertIssueStatus(t, issueID, "in_progress")
}
