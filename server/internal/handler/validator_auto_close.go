package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// tryAutoCloseAfterValidation is deliberately default-deny. It only consumes
// the structured fields written by CreateComment; comment prose is never read.
func (h *Handler) tryAutoCloseAfterValidation(ctx context.Context, issue db.Issue, verdict db.Comment, verifierID string) {
	openPRs, prErr := h.Queries.CountOpenPullRequestsByIssue(ctx, issue.ID)
	openChildren, childErr := h.Queries.CountOpenChildIssues(ctx, db.CountOpenChildIssuesParams{ParentIssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	failed := validationAutoCloseReasons(issue, verdict, verifierID, openPRs, openChildren)
	if prErr != nil {
		failed = append(failed, "open PR gate could not be checked")
	}
	if childErr != nil {
		failed = append(failed, "child issue gate could not be checked")
	}
	if len(failed) > 0 {
		// A failed automatic close is explicitly handed to the human queue and
		// leaves an audit trail rather than silently retaining in_review.
		waitingOn, _ := json.Marshal("ceo")
		if _, err := h.Queries.SetIssueMetadataKey(ctx, db.SetIssueMetadataKeyParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, Key: "waiting_on", Value: waitingOn}); err != nil {
			slog.Warn("validator waiting_on update failed", "issue_id", uuidToString(issue.ID), "error", err)
		}
		h.recordValidationAudit(ctx, issue, verdict.VerifierAgentID, fmt.Sprintf("Structured validator PASS %s did not auto-close: %s.", uuidToString(verdict.ID), strings.Join(failed, "; ")))
		return
	}
	updated, err := h.Queries.AutoCloseIssueAfterValidation(ctx, db.AutoCloseIssueAfterValidationParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		if err != pgx.ErrNoRows {
			slog.Warn("validator auto-close failed", "issue_id", uuidToString(issue.ID), "error", err)
		}
		return
	}
	h.recordValidationAudit(ctx, updated, verdict.VerifierAgentID, fmt.Sprintf("Auto-closed from structured validator PASS %s (verifier=%s, ref=%s, criteria=%s).", uuidToString(verdict.ID), verifierID, verdict.VerifiedRef.String, verdict.CriteriaVersion.String))
	h.notifyParentOfChildDone(ctx, issue, updated, "system", "")
	prefix := h.getIssuePrefix(ctx, issue.WorkspaceID)
	h.publish(protocol.EventIssueUpdated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{"issue": issueToResponse(updated, prefix), "status_changed": true, "prev_status": issue.Status, "source": "validator_pass"})
}

func validationAutoCloseReasons(issue db.Issue, verdict db.Comment, verifierID string, openPRs, openChildren int64) []string {
	failed := make([]string, 0, 6)
	if !issue.AutoCloseAllowed {
		failed = append(failed, "auto_close_allowed=false")
	}
	if issue.Status != "in_review" {
		failed = append(failed, "issue is not in_review")
	}
	if !issue.ImplementationAgentID.Valid || uuidToString(issue.ImplementationAgentID) == verifierID {
		failed = append(failed, "independent implementation agent is not recorded")
	}
	if !issue.CurrentRef.Valid || issue.CurrentRef.String != verdict.VerifiedRef.String {
		failed = append(failed, "verified_ref does not match current_ref")
	}
	if issue.ExternalValidationRequired {
		failed = append(failed, "external validation is required")
	}
	if openPRs > 0 {
		failed = append(failed, "linked PR is open")
	}
	if openChildren > 0 {
		failed = append(failed, "child issue is not terminal")
	}
	return failed
}

func (h *Handler) recordValidationAudit(ctx context.Context, issue db.Issue, authorID pgtype.UUID, body string) {
	_, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, AuthorType: "agent", AuthorID: authorID, Content: body, Type: "system"})
	if err != nil {
		slog.Warn("validator audit comment failed", "issue_id", uuidToString(issue.ID), "error", err)
	}
}
