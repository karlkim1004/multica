package handler

import (
	"net/http"
	"testing"
)

// routingProseComment reproduces the NEX-633 06:09 TeamLeader routing comment that
// triggered a case-3 false positive: it is routing/prose (not a verification verdict)
// but quotes the gate vocabulary ("validator PASS", "confidence") while explaining the
// gate rules, so the old substring heuristic mis-classified it as a confidence-based
// PASS claim and blocked a legitimate done transition.
const routingProseComment = `
[메타검증 + 클로즈 라우팅] validator 기계검증 PASS는 진성 — done 차단은 표준 PR 핸드오프 누락분

## 메타검증 결과 (validator PASS 검수)
validator(김채원) PASS는 실측 근거 기반 진성 검증으로 확인합니다(거짓검증 아님):
- NEX-605 run 패턴 DB 직접 재확인: 8 completed / 3 cancelled.
- "confidence 점수만"·"괜찮아보임"류 거짓검증 신호 없음 → 메타검증 통과.

## done 차단 원인 진단
status done이 prod Evidence Gate case-2에 막힌 것은 회귀/결함이 아니라 참 양성.

## 라우팅: be(카리나)
변경 증거(PR 링크 또는 metadata pr_url/commit)를 부착 후 done 전환을 진행해 주세요.
`

// F1 (false-positive removal): a routing/prose comment that merely cites gate
// vocabulary must NOT be classified as a final PASS comment, so its embedded
// "confidence" mention can no longer block a done transition.
func TestEvaluateFinalPassCommentIgnoresRoutingProseCitingGateVocabulary(t *testing.T) {
	result := evaluateFinalPassComment(routingProseComment)
	if result.IsFinalPass {
		t.Fatalf("routing/prose comment citing gate vocabulary must not be a final PASS comment, blockingReason=%q", result.BlockingReason)
	}
}

// F3 (no regression): a genuine confidence-only PASS verdict — a real PASS header
// claiming completion on a confidence score — must still be detected and rejected.
func TestEvaluateFinalPassCommentStillRejectsGenuineConfidencePass(t *testing.T) {
	comment := `
## QA PASS

라이브 증거: 화면 정상으로 보임
confidence: 0.95
결론: PASS
`
	result := evaluateFinalPassComment(comment)
	if !result.IsFinalPass {
		t.Fatalf("genuine confidence-only PASS verdict must remain a final PASS comment")
	}
	if result.Accepted {
		t.Fatalf("genuine confidence-only PASS verdict must be rejected")
	}
	if !containsFold(result.BlockingReason, "confidence") {
		t.Fatalf("blocking reason = %q, want it to cite confidence", result.BlockingReason)
	}
}

// A confidence-only claim declared on a bare verdict line (no PASS header) must also
// still be caught — structure, not a header keyword, is what makes it a verdict.
func TestEvaluateFinalPassCommentRejectsConfidencePassOnVerdictLine(t *testing.T) {
	comment := `
검증 결과 요약

결론: PASS (confidence 0.9 기반)
`
	result := evaluateFinalPassComment(comment)
	if !result.IsFinalPass {
		t.Fatalf("a verdict line declaring PASS must be a final PASS comment")
	}
	if result.Accepted {
		t.Fatalf("confidence-based verdict line must be rejected")
	}
}

// Strict accepted templates must keep passing after the refinement.
func TestEvaluateFinalPassCommentStillAcceptsStrictTemplateAfterRefinement(t *testing.T) {
	result := evaluateFinalPassComment(completeQAPassComment)
	if !result.IsFinalPass || !result.Accepted {
		t.Fatalf("strict QA PASS template must stay accepted, isFinalPass=%v accepted=%v reason=%q",
			result.IsFinalPass, result.Accepted, result.BlockingReason)
	}
}

// End-to-end: an issue whose latest comment is the routing/prose comment (and which
// carries valid done evidence and a clean run history) must reach done — the case-3
// false positive can no longer single-circuit the gate before case-2.
func TestUpdateIssueDoneAllowedWhenLatestCommentIsRoutingProse(t *testing.T) {
	issueID := createTestIssue(t, "Evidence gate case-3 routing prose false positive", "in_progress", "urgent")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	markIssueDoneEvidenceForTest(t, issueID)
	insertIssueCommentForTest(t, issueID, routingProseComment)

	w := attemptDoneTransition(t, issueID)
	if w.Code != http.StatusOK {
		t.Fatalf("routing-prose latest comment must not block done via case-3: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertIssueStatus(t, issueID, "done")
}

// End-to-end: a genuine confidence-only PASS comment must still block done (F3).
func TestUpdateIssueDoneBlockedByGenuineConfidencePassComment(t *testing.T) {
	issueID := createTestIssue(t, "Evidence gate case-3 genuine confidence pass", "in_progress", "urgent")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })
	markIssueDoneEvidenceForTest(t, issueID)
	insertIssueCommentForTest(t, issueID, `
## QA PASS

라이브 증거: 화면 정상
confidence: 0.93
결론: PASS
`)

	w := attemptDoneTransition(t, issueID)
	if w.Code != http.StatusConflict {
		t.Fatalf("genuine confidence-only PASS must still block done: expected 409, got %d: %s", w.Code, w.Body.String())
	}
	assertIssueStatus(t, issueID, "in_progress")
}
