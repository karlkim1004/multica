package handler

import (
	"net/http"

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
