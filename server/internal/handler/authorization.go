package handler

import (
	"net/http"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ResourceType is a workspace resource governed by member RBAC.
// Keep this list small: endpoint-specific resources are mapped to one of
// these categories by the Stage 2 handlers.
type ResourceType string

const (
	ResourceIssue ResourceType = "issue"
	ResourceSquad ResourceType = "squad"
	ResourceAgent ResourceType = "agent"
)

const (
	RoleOwner       = "owner"
	RoleAdmin       = "admin"
	RoleMember      = "member" // legacy role; policy-equivalent to super_user
	RoleSuperUser   = "super_user"
	RoleGeneralUser = "general_user"

	ActorMember = "member"
	ActorAgent  = "agent"
)

type authorizationActor struct {
	Type string
	ID   string
	Role string
}

type resourceOwner struct {
	CreatorType string
	CreatorID   string
}

func effectiveMemberRole(role string) string {
	if role == RoleMember {
		return RoleSuperUser
	}
	return role
}

// isHumanMember prevents task tokens from inheriting the owning human's role.
// Agent authorization remains deliberately separate from member RBAC.
func isHumanMember(actor authorizationActor) bool {
	return actor.Type == ActorMember && actor.ID != ""
}

func isAdmin(actor authorizationActor) bool {
	if !isHumanMember(actor) {
		return false
	}
	role := effectiveMemberRole(actor.Role)
	return role == RoleOwner || role == RoleAdmin
}

func isOwn(actor authorizationActor, owner resourceOwner) bool {
	return isHumanMember(actor) && owner.CreatorType == ActorMember && owner.CreatorID == actor.ID
}

func canRead(actor authorizationActor, resource ResourceType) bool {
	if !isHumanMember(actor) {
		return false
	}
	if isAdmin(actor) {
		return true
	}
	return effectiveMemberRole(actor.Role) == RoleSuperUser && resource == ResourceIssue
}

func canCreate(actor authorizationActor, resource ResourceType) bool {
	if !isHumanMember(actor) {
		return false
	}
	if isAdmin(actor) || effectiveMemberRole(actor.Role) == RoleSuperUser {
		return true
	}
	return effectiveMemberRole(actor.Role) == RoleGeneralUser && resource == ResourceIssue
}

func canMutate(actor authorizationActor, _ ResourceType, owner resourceOwner) bool {
	if isAdmin(actor) {
		return true
	}
	return effectiveMemberRole(actor.Role) == RoleSuperUser && isOwn(actor, owner)
}

// authorizeResource applies member RBAC to a resource operation. Agent task
// tokens keep their existing, separately-scoped authorization path: they are
// not silently treated as the backing human member.
func (h *Handler) authorizeResource(w http.ResponseWriter, r *http.Request, workspaceID string, resource ResourceType, action string, owner resourceOwner) bool {
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return false
	}
	if h.resourceAllowed(r, workspaceID, member, resource, action, owner) {
		return true
	}
	writeError(w, http.StatusForbidden, "insufficient permissions")
	return false
}

// RestrictGeneralUserWorkspaceRoutes closes the route-level gap between the
// resource handlers. A general_user is deliberately a write-only issue
// submitter: it may create an issue, but may not read or mutate any existing
// workspace resource (including issue-derived endpoints such as comments,
// metadata, labels, subscriptions, reactions, and task actions).
//
// This middleware is installed inside the workspace-membership group. Agent
// task tokens retain their existing daemon-scoped authorization and must not
// be reclassified as their backing human member here.
func (h *Handler) RestrictGeneralUserWorkspaceRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		workspaceID := h.resolveWorkspaceID(r)
		member, ok := h.workspaceMember(w, r, workspaceID)
		if !ok {
			return
		}
		actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID)
		if actorType == ActorAgent || effectiveMemberRole(member.Role) != RoleGeneralUser {
			next.ServeHTTP(w, r)
			return
		}

		// chi accepts both forms for the collection route.
		if r.Method == http.MethodPost && (r.URL.Path == "/api/issues" || r.URL.Path == "/api/issues/") {
			next.ServeHTTP(w, r)
			return
		}
		writeError(w, http.StatusForbidden, "insufficient permissions")
	})
}

// RestrictGeneralUserWorkspaceList applies the same write-only contract to
// the workspace directory, which has no single workspace context for the
// member middleware to inject. A caller whose memberships are all
// general_user may not enumerate workspaces; callers with an elevated
// membership retain their existing directory access for that workspace.
func (h *Handler) RestrictGeneralUserWorkspaceList(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, ok := requireUserID(w, r)
		if !ok {
			return
		}
		userUUID, err := util.ParseUUID(userID)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "user not authenticated")
			return
		}

		actorType, _ := h.resolveActor(r, userID, h.resolveWorkspaceID(r))
		if actorType == ActorAgent {
			next.ServeHTTP(w, r)
			return
		}

		workspaces, err := h.Queries.ListWorkspaces(r.Context(), userUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list workspaces")
			return
		}
		if len(workspaces) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		allGeneralUsers := true
		for _, workspace := range workspaces {
			member, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
				UserID:      userUUID,
				WorkspaceID: workspace.ID,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to resolve workspace membership")
				return
			}
			if effectiveMemberRole(member.Role) != RoleGeneralUser {
				allGeneralUsers = false
				break
			}
		}
		if allGeneralUsers {
			writeError(w, http.StatusForbidden, "insufficient permissions")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// resourceAllowed is the side-effect-free form used to preflight batch
// mutations. It lets a handler return a single truthful batch response before
// any rows are changed.
func (h *Handler) resourceAllowed(r *http.Request, workspaceID string, member db.Member, resource ResourceType, action string, owner resourceOwner) bool {
	actorType, actorID := h.resolveActor(r, requestUserID(r), workspaceID)
	if actorType == ActorAgent {
		return true
	}
	actor := authorizationActor{Type: ActorMember, ID: actorID, Role: member.Role}
	allowed := false
	switch action {
	case "read":
		allowed = canRead(actor, resource) || (effectiveMemberRole(actor.Role) == RoleSuperUser && isOwn(actor, owner))
	case "create":
		allowed = canCreate(actor, resource)
	case "mutate":
		allowed = canMutate(actor, resource, owner)
	}
	return allowed
}

func memberOwner(member db.Member) resourceOwner {
	return resourceOwner{CreatorType: ActorMember, CreatorID: uuidToString(member.UserID)}
}
