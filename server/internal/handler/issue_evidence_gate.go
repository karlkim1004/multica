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
	if reason := blockingTaskRunReason(tasks); reason != "" {
		return 409, reason
	}
	if status, msg := h.checkFinalPassCommentEvidence(ctx, issue, nextAgentCalled); status != 0 {
		return status, msg
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

func (h *Handler) checkFinalPassCommentEvidence(ctx context.Context, issue db.Issue, nextAgentCalled bool) (int, string) {
	comments, err := h.Queries.ListCommentsForIssue(ctx, db.ListCommentsForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		Limit:       commentHardCap,
	})
	if err != nil {
		return 500, "Evidence Gate 평가 중 PASS 코멘트 조회에 실패했습니다."
	}
	for i := len(comments) - 1; i >= 0; i-- {
		result := evaluateFinalPassComment(comments[i].Content)
		if !result.IsFinalPass {
			continue
		}
		if result.Accepted && finalPassRequiresNextAgentCall(comments[i].Content) && !nextAgentCalled && !containsAgentMention(comments[i].Content) {
			return 409, "Evidence Gate 차단: 다음 실행 주체가 필요한 PASS/done handoff인데 assignee 변경 또는 mention://agent/<UUID> 호출이 없습니다."
		}
		if !result.Accepted {
			return 409, result.BlockingReason
		}
		return 0, ""
	}
	return 0, ""
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

func blockingTaskRunReason(tasks []db.AgentTaskQueue) string {
	for _, task := range tasks {
		status := strings.ToLower(task.Status)
		failureReason := strings.ToLower(strings.TrimSpace(task.FailureReason.String))
		errorText := strings.ToLower(strings.TrimSpace(task.Error.String))
		resultText := strings.ToLower(string(task.Result))

		if status == "cancelled" {
			return fmt.Sprintf("Evidence Gate 차단: cancelled issue run이 존재합니다. task_id=%s", uuidToString(task.ID))
		}
		if status != "failed" {
			continue
		}
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
