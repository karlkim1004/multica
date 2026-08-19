package handler

import "testing"

func TestAuthorizationMatrix(t *testing.T) {
	resources := []ResourceType{ResourceIssue, ResourceSquad, ResourceAgent}
	roles := []struct {
		role      string
		issueRead bool
		allCreate bool
		allMutate bool
		ownMutate bool
	}{
		{RoleAdmin, true, true, true, true},
		{RoleOwner, true, true, true, true},
		{RoleSuperUser, true, true, false, true},
		{RoleMember, true, true, false, true}, // legacy member aliases super_user
		{RoleGeneralUser, false, false, false, false},
	}

	for _, tc := range roles {
		actor := authorizationActor{Type: ActorMember, ID: "actor", Role: tc.role}
		for _, resource := range resources {
			t.Run(tc.role+"/"+string(resource), func(t *testing.T) {
				wantRead := tc.issueRead && resource == ResourceIssue || tc.allMutate
				if got := canRead(actor, resource); got != wantRead {
					t.Errorf("canRead(%s) = %v, want %v", resource, got, wantRead)
				}
				wantCreate := tc.allCreate || tc.role == RoleGeneralUser && resource == ResourceIssue
				if got := canCreate(actor, resource); got != wantCreate {
					t.Errorf("canCreate(%s) = %v, want %v", resource, got, wantCreate)
				}
				if got := canMutate(actor, resource, resourceOwner{CreatorType: ActorMember, CreatorID: "actor"}); got != tc.ownMutate {
					t.Errorf("canMutate own %s = %v, want %v", resource, got, tc.ownMutate)
				}
				if got := canMutate(actor, resource, resourceOwner{CreatorType: ActorMember, CreatorID: "other"}); got != tc.allMutate {
					t.Errorf("canMutate other %s = %v, want %v", resource, got, tc.allMutate)
				}
			})
		}
	}
}

func TestAuthorizationDoesNotElevateAgentTaskToken(t *testing.T) {
	actor := authorizationActor{Type: ActorAgent, ID: "agent", Role: RoleAdmin}
	owner := resourceOwner{CreatorType: ActorMember, CreatorID: "owner"}
	for _, resource := range []ResourceType{ResourceIssue, ResourceSquad, ResourceAgent} {
		if canRead(actor, resource) || canCreate(actor, resource) || canMutate(actor, resource, owner) {
			t.Errorf("agent task token must not inherit member role for %s", resource)
		}
	}
}
