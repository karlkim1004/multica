package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type workspaceJoinRequestResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	UserID      string  `json:"user_id"`
	Status      string  `json:"status"`
	RequestedAt string  `json:"requested_at"`
	ReviewedAt  *string `json:"reviewed_at,omitempty"`
}

func workspaceJoinRequestToResponse(v db.WorkspaceJoinRequest) workspaceJoinRequestResponse {
	return workspaceJoinRequestResponse{ID: uuidToString(v.ID), WorkspaceID: uuidToString(v.WorkspaceID), UserID: uuidToString(v.UserID), Status: v.Status, RequestedAt: timestampToString(v.RequestedAt), ReviewedAt: timestampToPtr(v.ReviewedAt)}
}

// CreateWorkspaceJoinRequest is deliberately outside the workspace-membership
// middleware. The reusable join code proves the target workspace without
// exposing slugs to arbitrary authenticated accounts.
func (h *Handler) CreateWorkspaceJoinRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		JoinCode string `json:"join_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.JoinCode == "" {
		writeError(w, http.StatusBadRequest, "join_code is required")
		return
	}
	joinCode, err := h.Queries.GetActiveWorkspaceJoinCode(r.Context(), req.JoinCode)
	if err != nil {
		writeError(w, http.StatusNotFound, "join code not found")
		return
	}
	workspaceID := joinCode.WorkspaceID
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID := parseUUID(userID)
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{WorkspaceID: workspaceID, UserID: userUUID}); err == nil {
		writeError(w, http.StatusConflict, "already a workspace member")
		return
	}
	created, err := h.Queries.CreateWorkspaceJoinRequest(r.Context(), db.CreateWorkspaceJoinRequestParams{WorkspaceID: workspaceID, UserID: userUUID})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a join request is already pending")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create join request")
		return
	}
	writeJSON(w, http.StatusCreated, workspaceJoinRequestToResponse(created))
}

func (h *Handler) CreateWorkspaceJoinCode(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", RoleOwner, RoleAdmin)
	if !ok {
		return
	}
	code := randomID()
	if _, err := h.Queries.CreateWorkspaceJoinCode(r.Context(), db.CreateWorkspaceJoinCodeParams{WorkspaceID: member.WorkspaceID, Code: code, CreatedBy: member.UserID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create join code")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"join_code": code})
}

func (h *Handler) ListWorkspaceJoinRequests(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", RoleOwner, RoleAdmin)
	if !ok {
		return
	}
	requests, err := h.Queries.ListPendingWorkspaceJoinRequests(r.Context(), member.WorkspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list join requests")
		return
	}
	response := make([]workspaceJoinRequestResponse, len(requests))
	for i, request := range requests {
		response[i] = workspaceJoinRequestToResponse(request)
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) ApproveWorkspaceJoinRequest(w http.ResponseWriter, r *http.Request) {
	h.reviewWorkspaceJoinRequest(w, r, true)
}

func (h *Handler) RejectWorkspaceJoinRequest(w http.ResponseWriter, r *http.Request) {
	h.reviewWorkspaceJoinRequest(w, r, false)
}

func (h *Handler) reviewWorkspaceJoinRequest(w http.ResponseWriter, r *http.Request, approve bool) {
	workspaceID := workspaceIDFromURL(r, "id")
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", RoleOwner, RoleAdmin)
	if !ok {
		return
	}
	requestID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "requestId"), "join request id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to review join request")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	// Lock before inspecting status so concurrent approve retries observe the
	// first transaction's committed approved row and return it, rather than
	// both attempting the pending-only UPDATE.
	request, err := qtx.GetWorkspaceJoinRequestForUpdate(r.Context(), requestID)
	if err != nil || request.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "join request not found")
		return
	}
	if request.Status == "pending" {
		if approve {
			request, err = qtx.ApproveWorkspaceJoinRequest(r.Context(), db.ApproveWorkspaceJoinRequestParams{ID: request.ID, ReviewedBy: member.UserID})
			if err == nil {
				_, err = qtx.CreateMember(r.Context(), db.CreateMemberParams{WorkspaceID: request.WorkspaceID, UserID: request.UserID, Role: RoleGeneralUser})
				if isUniqueViolation(err) {
					err = nil
				}
			}
		} else {
			request, err = qtx.RejectWorkspaceJoinRequest(r.Context(), db.RejectWorkspaceJoinRequestParams{ID: request.ID, ReviewedBy: member.UserID})
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to review join request")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to review join request")
		return
	}
	if approve && request.Status == "approved" {
		h.MembershipCache.Invalidate(r.Context(), uuidToString(request.UserID), workspaceID)
	}
	writeJSON(w, http.StatusOK, workspaceJoinRequestToResponse(request))
}
