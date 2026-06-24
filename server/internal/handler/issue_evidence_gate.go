package handler

import (
	"context"
	"fmt"
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

func (h *Handler) checkDoneEvidenceGate(ctx context.Context, issue db.Issue) (int, string) {
	tasks, err := h.Queries.ListTasksByIssue(ctx, issue.ID)
	if err != nil {
		return 500, "Evidence Gate 평가 중 issue run 조회에 실패했습니다."
	}
	if reason := blockingTaskRunReason(tasks); reason != "" {
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
