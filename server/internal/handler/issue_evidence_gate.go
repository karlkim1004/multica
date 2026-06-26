package handler

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var doneEvidenceMetadataKeys = []string{
	"pr_url",
	"pr_number",
	"deploy_url",
	"commit_sha",
	"commit",
	"image",
	"image_tag",
	"deployment_image",
	"diff_url",
}

func shouldRunDoneEvidenceGate(prev db.Issue, nextStatus *string) bool {
	return nextStatus != nil && *nextStatus == "done" && prev.Status != "done"
}

func doneTransitionCallsNextAgent(prev db.Issue, nextAssigneeType, nextAssigneeID *string) bool {
	if nextAssigneeType == nil || nextAssigneeID == nil {
		return false
	}
	if *nextAssigneeType != "agent" || strings.TrimSpace(*nextAssigneeID) == "" {
		return false
	}
	return prev.AssigneeType.String != *nextAssigneeType || uuidToString(prev.AssigneeID) != *nextAssigneeID
}

func (h *Handler) checkDoneEvidenceGate(ctx context.Context, issue db.Issue, nextAgentCalled bool) (int, string) {
	tasks, err := h.Queries.ListTasksByIssue(ctx, issue.ID)
	if err != nil {
		return 500, "Evidence Gate 평가 중 issue run 조회에 실패했습니다."
	}

	// case-3 (final PASS comment parser) is evaluated first: a verification-complete
	// final PASS opens the close path (F2) that lets case-1 ignore accumulated benign
	// cancelled runs. A rejecting/handoff-incomplete PASS still blocks here.
	passStatus, passMsg, finalPassAccepted := h.checkFinalPassCommentEvidence(ctx, issue, nextAgentCalled)
	if passStatus != 0 {
		return passStatus, passMsg
	}

	// case-1 (run failures): failed runs always block; cancelled runs block only when
	// unrecovered, unless the verification-complete close path is open.
	if reason := blockingTaskRunReason(tasks, finalPassAccepted); reason != "" {
		return 409, reason
	}

	hasLinkedPR, err := h.issueHasLinkedPullRequest(ctx, issue)
	if err != nil {
		return 500, "Evidence Gate 평가 중 PR 증거 조회에 실패했습니다."
	}
	if !issueHasDoneEvidence(issue) && !hasLinkedPR {
		return 409, "Evidence Gate 차단: 변경 diff/PR 또는 배포 image/commit 증거가 없어 done 전환을 허용할 수 없습니다. 변경 없음은 no-op/검증 대상 없음으로 분리해야 합니다."
	}
	return 0, ""
}

type finalPassCommentEvaluation struct {
	IsFinalPass    bool
	Accepted       bool
	MissingFields  []string
	BlockingReason string
}

// checkFinalPassCommentEvidence evaluates the most recent final-PASS comment. It
// returns (status, msg) for a blocking decision, and accepted=true only when a
// verification-complete final PASS was found and fully accepted (no rejection, no
// missing handoff) — that flag opens the case-1 close path (F2).
func (h *Handler) checkFinalPassCommentEvidence(ctx context.Context, issue db.Issue, nextAgentCalled bool) (int, string, bool) {
	comments, err := h.Queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Limit:       commentHardCap,
	})
	if err != nil {
		return 500, "Evidence Gate 평가 중 PASS 코멘트 조회에 실패했습니다.", false
	}
	for i := len(comments) - 1; i >= 0; i-- {
		result := evaluateFinalPassComment(comments[i].Content)
		if !result.IsFinalPass {
			continue
		}
		if result.Accepted && finalPassRequiresNextAgentCall(comments[i].Content) && !nextAgentCalled && !containsAgentMention(comments[i].Content) {
			return 409, "Evidence Gate 차단: 다음 실행 주체가 필요한 PASS/done handoff인데 assignee 변경 또는 mention://agent/<UUID> 호출이 없습니다.", false
		}
		if !result.Accepted {
			return 409, result.BlockingReason, false
		}
		return 0, "", true
	}
	return 0, "", false
}

var agentMentionRe = regexp.MustCompile(`mention://agent/[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func containsAgentMention(content string) bool {
	return agentMentionRe.MatchString(content)
}

func finalPassRequiresNextAgentCall(content string) bool {
	normalized := strings.ToLower(content)
	for _, phrase := range []string{
		"다음 담당자",
		"다음봇",
		"다음 봇",
		"다음 실행 주체",
		"다음 1수",
		"handoff required",
		"handoff:",
		"handoff 대상",
	} {
		if strings.Contains(normalized, phrase) {
			return true
		}
	}
	return false
}

func evaluateFinalPassComment(content string) finalPassCommentEvaluation {
	normalized := strings.ToLower(content)
	result := finalPassCommentEvaluation{
		IsFinalPass: looksLikeFinalPassComment(normalized),
		Accepted:    true,
	}
	if !result.IsFinalPass {
		return result
	}
	if strings.Contains(normalized, "conditional_pass") {
		return rejectedFinalPassComment("Evidence Gate 차단: CONDITIONAL_PASS는 최종 PASS로 인정할 수 없습니다.")
	}
	if strings.Contains(normalized, "confidence") {
		return rejectedFinalPassComment("Evidence Gate 차단: confidence 점수는 최종 PASS 근거로 인정할 수 없습니다.")
	}
	if isHTTP200OnlyEvidence(normalized) {
		return rejectedFinalPassComment("Evidence Gate 차단: HTTP 200 단독 확인은 최종 PASS 라이브 증거로 인정할 수 없습니다.")
	}
	if reason := disqualifyingPassEvidenceReason(normalized); reason != "" {
		return rejectedFinalPassComment(reason)
	}

	switch finalPassTemplateType(normalized) {
	case "validator":
		return evaluateValidatorPassTemplate(normalized)
	default:
		return evaluateQAPassTemplate(normalized)
	}
}

func rejectedFinalPassComment(reason string) finalPassCommentEvaluation {
	return finalPassCommentEvaluation{
		IsFinalPass:    true,
		Accepted:       false,
		BlockingReason: reason,
	}
}

func looksLikeFinalPassComment(normalized string) bool {
	if !strings.Contains(normalized, "pass") {
		return false
	}
	if strings.Contains(normalized, "conditional_pass") {
		return true
	}
	return strings.Contains(normalized, "결론") ||
		strings.Contains(normalized, "qa pass") ||
		strings.Contains(normalized, "validator pass")
}

func finalPassTemplateType(normalized string) string {
	for _, line := range strings.Split(normalized, "\n") {
		header := strings.TrimLeft(strings.TrimSpace(line), "#>*- \t")
		switch {
		case isPassHeader(header, "qa pass"):
			return "qa"
		case isPassHeader(header, "validator pass"):
			return "validator"
		}
	}
	return "qa"
}

func isPassHeader(line, marker string) bool {
	if line == marker {
		return true
	}
	return strings.HasPrefix(line, marker+" ") ||
		strings.HasPrefix(line, marker+":") ||
		strings.HasPrefix(line, marker+"(")
}

func evaluateQAPassTemplate(normalized string) finalPassCommentEvaluation {
	required := []string{
		"변경 대상",
		"실행 테스트",
		"사용자 시나리오",
		"라이브 증거",
		"실패 run scan",
		"결론",
	}
	missing := missingRequiredTerms(normalized, required)
	if len(missing) == 0 {
		return finalPassCommentEvaluation{IsFinalPass: true, Accepted: true}
	}
	return finalPassCommentEvaluation{
		IsFinalPass:    true,
		Accepted:       false,
		MissingFields:  missing,
		BlockingReason: "Evidence Gate 차단: QA PASS 최소 증거 템플릿 누락: " + strings.Join(missing, ", "),
	}
}

func evaluateValidatorPassTemplate(normalized string) finalPassCommentEvaluation {
	required := []string{
		"surface",
		"변경 여부",
		"영향 범위",
		"검증 증거",
		"회귀 위험",
		"판정",
		"issue status transition",
		"run failure scan",
		"comment pass parser",
		"no-op handling",
		"parent/child issue workflow",
		"pr close intent workflow",
	}
	missing := missingRequiredTerms(normalized, required)
	if len(missing) == 0 {
		return finalPassCommentEvaluation{IsFinalPass: true, Accepted: true}
	}
	return finalPassCommentEvaluation{
		IsFinalPass:    true,
		Accepted:       false,
		MissingFields:  missing,
		BlockingReason: "Evidence Gate 차단: validator 전체 시스템 영향 범위 matrix 누락: " + strings.Join(missing, ", "),
	}
}

func missingRequiredTerms(normalized string, required []string) []string {
	var missing []string
	for _, term := range required {
		if !strings.Contains(normalized, strings.ToLower(term)) {
			missing = append(missing, term)
		}
	}
	return missing
}

func isHTTP200OnlyEvidence(normalized string) bool {
	if !strings.Contains(normalized, "http 200") {
		return false
	}
	for _, richer := range []string{
		"dom",
		"스크린샷",
		"screenshot",
		"hash",
		"해시",
		"log",
		"로그",
		"응답 본문",
		"body",
		"http 409",
		"http 404",
	} {
		if strings.Contains(normalized, richer) {
			return false
		}
	}
	return true
}

func disqualifyingPassEvidenceReason(normalized string) string {
	for _, phrase := range []string{
		"산출물 부재",
		"부분 검증",
		"부분검증",
		"일부 화면만 확인",
		"dry-run 증거 없음",
		"dry run 증거 없음",
		"드라이런 증거 없음",
	} {
		if strings.Contains(normalized, phrase) {
			return "Evidence Gate 차단: " + phrase + " 상태는 최종 PASS 근거로 인정할 수 없습니다."
		}
	}
	return ""
}

func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// blockingTaskRunReason returns a non-empty reason when an issue's run history must
// block a done transition (Evidence Gate case-1).
//
//   - failed runs always block (true positive — genuine verification failure).
//   - a cancelled run blocks only when it was never recovered, i.e. no completed run
//     was created after it. benign re-invocation (cancel → re-dispatch → complete),
//     which is pervasive in multi-agent operation (@mention wake-ups, chain-keeper /
//     TeamLeader re-routing), leaves a completed run after the cancel and is excused.
//
// When allowBenignCancelled is true (verification-complete close path, F2), every
// cancelled run is ignored regardless of recovery; failed runs still block.
func blockingTaskRunReason(tasks []db.AgentTaskQueue, allowBenignCancelled bool) string {
	if reason := failedRunReason(tasks); reason != "" {
		return reason
	}
	if allowBenignCancelled {
		return ""
	}
	return unrecoveredCancelledRunReason(tasks)
}

func failedRunReason(tasks []db.AgentTaskQueue) string {
	for _, task := range tasks {
		if strings.ToLower(task.Status) != "failed" {
			continue
		}
		failureReason := strings.ToLower(strings.TrimSpace(task.FailureReason.String))
		errorText := strings.ToLower(strings.TrimSpace(task.Error.String))
		resultText := strings.ToLower(string(task.Result))
		if failureReason == "" {
			failureReason = "failed"
		}
		if failureReason == "api_invalid_request" ||
			failureReason == "agent_error" ||
			strings.Contains(errorText, "unsupported model") ||
			strings.Contains(resultText, "unsupported model") {
			return fmt.Sprintf("Evidence Gate 차단: 실패한 issue run이 존재합니다. task_id=%s, failure_reason=%s", uuidToString(task.ID), failureReason)
		}
		return fmt.Sprintf("Evidence Gate 차단: failed issue run이 존재합니다. task_id=%s, failure_reason=%s", uuidToString(task.ID), failureReason)
	}
	return ""
}

func unrecoveredCancelledRunReason(tasks []db.AgentTaskQueue) string {
	for _, task := range tasks {
		if strings.ToLower(task.Status) != "cancelled" {
			continue
		}
		if hasCompletedRunAfter(tasks, task) {
			continue
		}
		return fmt.Sprintf("Evidence Gate 차단: 복구되지 않은 cancelled issue run이 존재합니다 (이후 completed run 없음). task_id=%s", uuidToString(task.ID))
	}
	return ""
}

// hasCompletedRunAfter reports whether a completed run was created strictly after the
// given cancelled run — the signal that the cancel was a benign re-invocation that
// subsequently succeeded.
func hasCompletedRunAfter(tasks []db.AgentTaskQueue, cancelled db.AgentTaskQueue) bool {
	if !cancelled.CreatedAt.Valid {
		return false
	}
	for _, task := range tasks {
		if strings.ToLower(task.Status) != "completed" {
			continue
		}
		if task.CreatedAt.Valid && task.CreatedAt.Time.After(cancelled.CreatedAt.Time) {
			return true
		}
	}
	return false
}

func issueHasDoneEvidence(issue db.Issue) bool {
	metadata := parseIssueMetadata(issue.Metadata)
	for _, key := range doneEvidenceMetadataKeys {
		if hasNonEmptyPrimitive(metadata[key]) {
			return true
		}
	}
	return false
}

func (h *Handler) issueHasLinkedPullRequest(ctx context.Context, issue db.Issue) (bool, error) {
	prs, err := h.Queries.ListPullRequestsByIssue(ctx, issue.ID)
	if err != nil {
		return false, err
	}
	return len(prs) > 0, nil
}

func hasNonEmptyPrimitive(v any) bool {
	switch value := v.(type) {
	case string:
		return strings.TrimSpace(value) != ""
	case bool:
		return value
	case float64:
		return true
	default:
		return false
	}
}
