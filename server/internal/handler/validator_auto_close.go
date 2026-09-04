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
	// All authority and lifecycle gates are re-evaluated by the one conditional
	// UPDATE below.  Do not turn the advisory reason calculation into a close
	// decision: that would recreate the TOCTOU window this finalizer removes.
	updated, err := h.Queries.AutoCloseIssueAfterValidation(ctx, db.AutoCloseIssueAfterValidationParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID, CommentID: verdict.ID})
	if err != nil {
		if err == pgx.ErrNoRows {
			openPRs, _ := h.Queries.CountOpenPullRequestsByIssue(ctx, issue.ID)
			openChildren, _ := h.Queries.CountOpenChildIssues(ctx, db.CountOpenChildIssuesParams{ParentIssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
			failed := validationAutoCloseReasons(issue, verdict, verifierID, openPRs, openChildren)
			validator, validatorErr := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{ID: verdict.VerifierAgentID, WorkspaceID: issue.WorkspaceID})
			if validatorErr != nil || validator.ArchivedAt.Valid || !validator.IsValidator {
				failed = append(failed, "verifier is not a registered active validator")
			}
			if len(failed) == 0 {
				failed = append(failed, "an eligibility gate changed before the conditional update")
			}
			waitingOn, _ := json.Marshal("ceo")
			if _, metaErr := h.Queries.SetIssueMetadataKey(ctx, db.SetIssueMetadataKeyParams{ID: issue.ID, WorkspaceID: issue.WorkspaceID, Key: "waiting_on", Value: waitingOn}); metaErr != nil {
				slog.Warn("validator waiting_on update failed", "issue_id", uuidToString(issue.ID), "error", metaErr)
			}
			h.recordValidationAudit(ctx, issue, verdict.VerifierAgentID, fmt.Sprintf("Structured validator PASS %s did not auto-close: %s.", uuidToString(verdict.ID), strings.Join(failed, "; ")))
			return
		}
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
	if !issue.AutoCloseCriteriaVersion.Valid || issue.AutoCloseCriteriaVersion.String != verdict.CriteriaVersion.String {
		failed = append(failed, "criteria_version does not match auto-close criteria")
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
