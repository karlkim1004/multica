package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEvaluateFinalPassCommentRequiresQATemplateEvidence(t *testing.T) {
	comment := `
## QA PASS

변경 대상: PR https://github.com/karlkim1004/multica/pull/12
실행 테스트: go test ./internal/handler -run TestEvidenceGate -count=1 PASS
사용자 시나리오: done 전환 차단 시나리오 PASS
라이브 증거: HTTP 409 차단 응답과 parser fixture log 확인
실패 run scan: failed/cancelled/api_invalid_request/unsupported model 없음
결론: PASS
`

	result := evaluateFinalPassComment(comment)
	if !result.Accepted {
		t.Fatalf("complete QA PASS template should be accepted, missing=%v reason=%q", result.MissingFields, result.BlockingReason)
	}
}

func TestEvaluateFinalPassCommentTreatsQAPassHeaderAsQAEvenWhenBodyMentionsValidator(t *testing.T) {
	comment := `
## QA PASS

변경 대상: QA/validator PASS 템플릿 파서
실행 테스트: go test ./internal/handler -run TestEvaluateFinalPassComment -count=1 PASS
사용자 시나리오: done 전환 경로 PASS
라이브 증거: HTTP 409 차단 응답 body 확인
실패 run scan: failed/cancelled/api_invalid_request/unsupported model 없음
결론: PASS
`

	result := evaluateFinalPassComment(comment)
	if !result.Accepted {
		t.Fatalf("QA PASS header should use QA template even when body mentions validator, missing=%v reason=%q", result.MissingFields, result.BlockingReason)
	}
}

func TestEvaluateFinalPassCommentRejectsConditionalPassConfidenceAndHTTP200Only(t *testing.T) {
	cases := []struct {
		name    string
		comment string
		want    string
	}{
		{
			name: "conditional pass",
			comment: `
## QA
변경 대상: PR 있음
실행 테스트: curl 반환 확인
사용자 시나리오: 일부 확인
라이브 증거: HTTP 200
실패 run scan: 미확인
결론: CONDITIONAL_PASS
`,
			want: "CONDITIONAL_PASS",
		},
		{
			name: "confidence score",
			comment: `
## QA PASS
confidence: 0.92
결론: PASS
`,
			want: "confidence",
		},
		{
			name: "http 200 only",
			comment: `
## QA PASS
라이브 증거: HTTP 200
결론: PASS
`,
			want: "HTTP 200 단독",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := evaluateFinalPassComment(tc.comment)
			if result.Accepted {
				t.Fatalf("comment should be rejected")
			}
			if result.BlockingReason == "" || !containsFold(result.BlockingReason, tc.want) {
				t.Fatalf("blocking reason = %q, want it to mention %q", result.BlockingReason, tc.want)
			}
		})
	}
}

func TestEvaluateFinalPassCommentRequiresValidatorImpactMatrix(t *testing.T) {
	comment := `
## Validator PASS

Surface | 변경 여부 | 영향 범위 | 검증 증거 | 회귀 위험 | 판정
Issue status transition | required | done side effect | test log | High | PASS
Run failure scan | required | failed/cancelled run | fixture | High | PASS
Comment PASS parser | required | QA/validator comment | parser test | Medium | PASS
No-op handling | required | 변경 없음 케이스 | fixture | High | PASS
Parent/child issue workflow | required | child done wake | integration test | Medium | PASS
PR close intent workflow | required | merged PR auto-close | webhook test | Medium | PASS

결론: PASS
`

	result := evaluateFinalPassComment(comment)
	if !result.Accepted {
		t.Fatalf("complete validator impact matrix should be accepted, missing=%v reason=%q", result.MissingFields, result.BlockingReason)
	}

	incomplete := `
## Validator PASS
Surface | 판정
Issue status transition | PASS
결론: PASS
`
	result = evaluateFinalPassComment(incomplete)
	if result.Accepted {
		t.Fatalf("incomplete validator matrix should be rejected")
	}
	if !containsFold(result.BlockingReason, "전체 시스템 영향 범위 matrix") {
		t.Fatalf("blocking reason = %q, want validator matrix reason", result.BlockingReason)
	}
}

func TestUpdateIssueDoneRejectsIncompleteQAPassComment(t *testing.T) {
	ctx := context.Background()
	issueID := createTestIssue(t, "Evidence gate incomplete QA PASS", "in_progress", "urgent")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	markIssueDoneEvidenceForTest(t, issueID)
	insertIssueCommentForTest(t, issueID, `
## QA PASS
라이브 증거: HTTP 200
결론: PASS
`)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"status": "done",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("UpdateIssue done with incomplete QA PASS: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "HTTP 200 단독") {
		t.Fatalf("blocking reason should mention HTTP 200 only evidence, got %s", w.Body.String())
	}

	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	if status != "in_progress" {
		t.Fatalf("blocked transition changed status to %q", status)
	}
}

func TestUpdateIssueDoneAllowsCompleteQAPassComment(t *testing.T) {
	issueID := createTestIssue(t, "Evidence gate complete QA PASS", "in_progress", "urgent")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	markIssueDoneEvidenceForTest(t, issueID)
	insertIssueCommentForTest(t, issueID, `
## QA PASS

변경 대상: commit handler-test-evidence
실행 테스트: go test ./internal/handler -run TestUpdateIssueDoneAllowsCompleteQAPassComment -count=1 PASS
사용자 시나리오: done 전환 경로 PASS
라이브 증거: HTTP 200 응답 본문과 DB status done 확인
실패 run scan: failed/cancelled/api_invalid_request/unsupported model 없음
결론: PASS
`)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"status": "done",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue done with complete QA PASS: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated IssueResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated issue: %v", err)
	}
	if updated.Status != "done" {
		t.Fatalf("status = %q, want done", updated.Status)
	}
}

func TestUpdateIssueDoneRejectsFinalPassHandoffWithoutAgentMention(t *testing.T) {
	ctx := context.Background()
	issueID := createTestIssue(t, "Evidence gate missing next agent call", "in_progress", "urgent")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	markIssueDoneEvidenceForTest(t, issueID)
	insertIssueCommentForTest(t, issueID, `
## QA PASS

변경 대상: commit handler-test-handoff
실행 테스트: go test ./internal/handler -run TestUpdateIssueDoneRejectsFinalPassHandoffWithoutAgentMention -count=1 PASS
사용자 시나리오: done 전환 경로 PASS
라이브 증거: HTTP 409 응답 본문과 DB status 유지 확인
실패 run scan: failed/cancelled/api_invalid_request/unsupported model 없음
다음 담당자: 쵸단(qa)가 검증을 이어받아야 함
다음 1수: QA 재검증
결론: PASS
`)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"status": "done",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("UpdateIssue done with handoff missing mention: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "다음 실행 주체") {
		t.Fatalf("blocking reason should mention next execution owner, got %s", w.Body.String())
	}

	var status string
	if err := testPool.QueryRow(ctx, `SELECT status FROM issue WHERE id = $1`, issueID).Scan(&status); err != nil {
		t.Fatalf("load issue status: %v", err)
	}
	if status != "in_progress" {
		t.Fatalf("blocked transition changed status to %q", status)
	}
}

func TestUpdateIssueDoneAllowsFinalPassHandoffWithAgentMention(t *testing.T) {
	agentID := createHandlerTestAgent(t, "Evidence Gate Next QA", nil)
	issueID := createTestIssue(t, "Evidence gate next agent called", "in_progress", "urgent")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	markIssueDoneEvidenceForTest(t, issueID)
	insertIssueCommentForTest(t, issueID, `
## QA PASS

변경 대상: commit handler-test-handoff
실행 테스트: go test ./internal/handler -run TestUpdateIssueDoneAllowsFinalPassHandoffWithAgentMention -count=1 PASS
사용자 시나리오: done 전환 경로 PASS
라이브 증거: HTTP 200 응답 본문과 DB status done 확인
실패 run scan: failed/cancelled/api_invalid_request/unsupported model 없음
다음 담당자: [`+agentID+`](mention://agent/`+agentID+`) 검증 이어받기
다음 1수: QA 재검증
결론: PASS
`)

	w := httptest.NewRecorder()
	req := newRequest("PUT", "/api/issues/"+issueID, map[string]any{
		"status": "done",
	})
	req = withURLParam(req, "id", issueID)
	testHandler.UpdateIssue(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("UpdateIssue done with handoff mention: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func markIssueDoneEvidenceForTest(t *testing.T, issueID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE issue SET metadata = COALESCE(metadata, '{}'::jsonb) || '{"pr_url": "https://github.com/test/test/pull/1"}'::jsonb WHERE id = $1`,
		issueID,
	); err != nil {
		t.Fatalf("mark issue done evidence for test: %v", err)
	}
}

func insertIssueCommentForTest(t *testing.T, issueID, content string) {
	t.Helper()

	var agentID string
	if err := testPool.QueryRow(context.Background(), `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("load test agent: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type)
		VALUES ($1, $2, 'agent', $3, $4, 'comment')
	`, issueID, testWorkspaceID, agentID, content); err != nil {
		t.Fatalf("insert issue comment: %v", err)
	}
}
