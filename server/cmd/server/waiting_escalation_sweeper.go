package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// waitingRecallAfterSeconds / waitingReassignAfterSeconds implement the
	// NEX-1043 ownership SLA: an issue whose metadata says an agent is
	// holding the ball (waiting_on = "agent:<id>") gets recalled 48h after
	// waiting_since, then reassigned to the CEO queue 24h after that recall
	// if nothing on the issue changed in between. waiting_on = "ceo" never
	// matches either sweep (see issue_waiting_escalation.sql), which is what
	// keeps the CEO queue silent per condition 5.
	waitingRecallAfterSeconds   = 48 * 3600.0
	waitingReassignAfterSeconds = 24 * 3600.0
)

// waitingEscalationAuthorAgentID authors the automated recall/reassign
// comments this sweeper posts. It reuses 아이유(TeamLeader)'s id — the same
// constant internal/handler/comment.go calls teamLeaderMentionUUID and uses
// as the default reroute target for bot-authored escalations — because that
// is already this workspace's established actor for platform-automated
// "재호출" messages (see the 2026-08-27 bulk-recall comment on NEX-1043
// itself, authored by this same id).
const waitingEscalationAuthorAgentID = "a9b0fb13-bfaf-4cea-a6e4-d27e243ec2b0"

// sweepWaitingEscalations runs both NEX-1043 SLA stages. Called from the
// runtime sweeper's existing ticker (runtime_sweeper.go) rather than a
// dedicated one — same cadence is generously fine for a 48h/24h SLA.
func sweepWaitingEscalations(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService) {
	sweepWaitingRecalls(ctx, queries, taskSvc)
	sweepWaitingReassignments(ctx, queries)
}

// sweepWaitingRecalls finds issues whose waiting_since has exceeded the
// recall threshold with no recall issued yet, and recalls each one.
func sweepWaitingRecalls(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService) {
	candidates, err := queries.SelectIssuesNeedingWaitingRecall(ctx, waitingRecallAfterSeconds)
	if err != nil {
		slog.Warn("waiting escalation: failed to list recall candidates", "error", err)
		return
	}
	for _, issue := range candidates {
		recallWaitingIssue(ctx, queries, taskSvc, issue)
	}
}

// recallWaitingIssue posts a system comment mentioning the agent holding the
// wait, enqueues a task so the recall actually triggers a new run (posting
// alone would not — this bypasses the HTTP comment endpoint's
// triggerTasksForComment path), and stamps escalation_recalled_at so the
// next tick doesn't resend.
func recallWaitingIssue(ctx context.Context, queries *db.Queries, taskSvc *service.TaskService, issue db.Issue) {
	issueIDStr := util.UUIDToString(issue.ID)
	meta := parseWaitingMetadata(issue.Metadata)
	waitingOn := meta["waiting_on"]
	agentIDStr := strings.TrimPrefix(waitingOn, "agent:")
	agentID, err := util.ParseUUID(agentIDStr)
	if err != nil {
		slog.Warn("waiting escalation: invalid waiting_on agent id, skipping recall", "issue_id", issueIDStr, "waiting_on", waitingOn)
		return
	}

	content := fmt.Sprintf(
		"⏰ NEX-1043 자동 재호출: `waiting_on=agent:%s`가 48시간을 초과했습니다 (waiting_since=%s).\n\nunblock_condition: %s\n\n[@담당 봇](mention://agent/%s) 진행 상황을 갱신하거나 완료하십시오. 이후 24시간 동안 변화가 없으면 대표님 큐(`waiting_on=ceo`)로 자동 재배정됩니다.",
		agentIDStr, meta["waiting_since"], meta["unblock_condition"], agentIDStr,
	)
	comment, err := queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    waitingEscalationAuthorID(),
		Content:     content,
		Type:        "system",
	})
	if err != nil {
		slog.Warn("waiting escalation: failed to post recall comment", "issue_id", issueIDStr, "error", err)
		return
	}

	if _, err := taskSvc.EnqueueTaskForMention(ctx, issue, agentID, comment.ID); err != nil {
		// Non-fatal: the comment above still records the recall attempt for
		// audit purposes, and stamping escalation_recalled_at below still
		// prevents a resend loop every tick regardless of enqueue outcome
		// (e.g. the agent was archived since the wait started).
		slog.Warn("waiting escalation: failed to enqueue recall task", "issue_id", issueIDStr, "agent_id", agentIDStr, "error", err)
	}

	recalledAtJSON, _ := json.Marshal(time.Now().UTC().Format(time.RFC3339))
	if _, err := queries.SetIssueMetadataKey(ctx, db.SetIssueMetadataKeyParams{
		ID: issue.ID, WorkspaceID: issue.WorkspaceID, Key: "escalation_recalled_at", Value: recalledAtJSON,
	}); err != nil {
		slog.Warn("waiting escalation: failed to record recall timestamp", "issue_id", issueIDStr, "error", err)
		return
	}

	slog.Info("waiting escalation: recalled agent", "issue_id", issueIDStr, "agent_id", agentIDStr, "waiting_since", meta["waiting_since"])
}

// sweepWaitingReassignments finds recalled issues where the reassign
// threshold has since elapsed with no observable change, and reassigns each
// one to the CEO queue.
func sweepWaitingReassignments(ctx context.Context, queries *db.Queries) {
	candidates, err := queries.SelectIssuesNeedingWaitingReassign(ctx, waitingReassignAfterSeconds)
	if err != nil {
		slog.Warn("waiting escalation: failed to list reassign candidates", "error", err)
		return
	}
	for _, issue := range candidates {
		reassignWaitingIssue(ctx, queries, issue)
	}
}

// reassignWaitingIssue moves ownership to the CEO queue (waiting_on=ceo) and
// leaves an audit comment. It deliberately does NOT @mention a human here:
// auto-notifying a person the moment an issue enters the CEO queue is
// exactly what NEX-1043 completion condition 5 forbids — waiting_on=ceo
// issues are meant to be pulled from the single CEO view, never pushed.
func reassignWaitingIssue(ctx context.Context, queries *db.Queries, issue db.Issue) {
	issueIDStr := util.UUIDToString(issue.ID)
	meta := parseWaitingMetadata(issue.Metadata)
	previousWaitingOn := meta["waiting_on"]
	nowStr := time.Now().UTC().Format(time.RFC3339)

	updated, err := queries.ReassignIssueWaitingToCeo(ctx, db.ReassignIssueWaitingToCeoParams{
		UnblockCondition: fmt.Sprintf("대표님 확인 필요 — 48h 재호출 후 24h 무응답 (기존 담당 %s)", previousWaitingOn),
		WaitingSince:     nowStr,
		ReassignedFrom:   previousWaitingOn,
		ReassignedAt:     nowStr,
		ID:               issue.ID,
		WorkspaceID:      issue.WorkspaceID,
	})
	if err != nil {
		slog.Warn("waiting escalation: failed to reassign issue to ceo queue", "issue_id", issueIDStr, "error", err)
		return
	}

	content := fmt.Sprintf(
		"🔁 NEX-1043 자동 재배정: 48시간 재호출 후 24시간 동안 변화가 없어 `waiting_on=ceo`로 전환했습니다 (기존: `%s`).",
		previousWaitingOn,
	)
	if _, err := queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     updated.ID,
		WorkspaceID: updated.WorkspaceID,
		AuthorType:  "agent",
		AuthorID:    waitingEscalationAuthorID(),
		Content:     content,
		Type:        "system",
	}); err != nil {
		slog.Warn("waiting escalation: failed to post reassign comment", "issue_id", issueIDStr, "error", err)
	}

	slog.Info("waiting escalation: reassigned to ceo queue", "issue_id", issueIDStr, "previous_waiting_on", previousWaitingOn)
}

func waitingEscalationAuthorID() pgtype.UUID {
	id, _ := util.ParseUUID(waitingEscalationAuthorAgentID)
	return id
}

// parseWaitingMetadata decodes the NEX-1043 ownership keys out of an issue's
// metadata JSONB, degrading to an empty map on malformed/empty input (same
// tolerance as internal/handler/issue_metadata.go's parseIssueMetadata).
func parseWaitingMetadata(raw []byte) map[string]string {
	out := map[string]string{}
	if len(raw) == 0 {
		return out
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return out
	}
	for _, k := range []string{"waiting_on", "unblock_condition", "waiting_since"} {
		if s, ok := parsed[k].(string); ok {
			out[k] = s
		}
	}
	return out
}
