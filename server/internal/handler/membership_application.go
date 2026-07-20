package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CreateMembershipApplication lets an authenticated OAuth user ask to join a
// specific workspace. It does not create a member or grant workspace access.
func (h *Handler) CreateMembershipApplication(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, workspaceIDFromURL(r, "id"), "workspace id")
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{WorkspaceID: workspaceID, UserID: userUUID}); err == nil {
		writeError(w, http.StatusConflict, "user is already a member")
		return
	}
	var applicationID string
	err := h.DB.QueryRow(r.Context(), `
		INSERT INTO workspace_membership_application (workspace_id, user_id)
		VALUES ($1, $2) RETURNING id
	`, workspaceID, userUUID).Scan(&applicationID)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "membership application already pending")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create membership application")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": applicationID, "status": "pending"})
}

// ApproveMembershipApplication atomically turns one pending application into
// a general-member membership. Row locking makes a second concurrent approval
// observe the completed state rather than creating a second membership.
func (h *Handler) ApproveMembershipApplication(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "admin role required")
		return
	}

	applicationID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "applicationId"), "application id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve membership application")
		return
	}
	defer tx.Rollback(r.Context())

	var applicationWorkspaceID, applicantID string
	var status string
	err = tx.QueryRow(r.Context(), `
		SELECT workspace_id, user_id, status
		FROM workspace_membership_application
		WHERE id = $1
		FOR UPDATE
	`, applicationID).Scan(&applicationWorkspaceID, &applicantID, &status)
	if err != nil {
		writeError(w, http.StatusNotFound, "membership application not found")
		return
	}
	if applicationWorkspaceID != workspaceID {
		writeError(w, http.StatusNotFound, "membership application not found")
		return
	}
	if status != "pending" {
		writeError(w, http.StatusConflict, "membership application is not pending")
		return
	}

	if _, err = tx.Exec(r.Context(), `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
		ON CONFLICT (workspace_id, user_id) DO NOTHING
	`, applicationWorkspaceID, applicantID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create membership")
		return
	}
	if _, err = tx.Exec(r.Context(), `
		UPDATE workspace_membership_application
		SET status = 'approved', reviewed_by = $2, reviewed_at = now(), updated_at = now()
		WHERE id = $1
	`, applicationID, member.UserID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve membership application")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve membership application")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}
