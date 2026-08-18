package handler

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
