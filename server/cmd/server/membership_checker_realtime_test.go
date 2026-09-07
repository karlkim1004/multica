package main

import (
	"context"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestMembershipChecker_CanReceiveWorkspaceEvents pins the realtime read gate
// against the real role strings stored in the member table. hub_test.go
// already covers the hub's handling of a MembershipChecker's answer; this
// test covers the answer itself, since a role-string typo here would defeat
// that hub-level enforcement silently.
func TestMembershipChecker_CanReceiveWorkspaceEvents(t *testing.T) {
	if testPool == nil {
		t.Skip("no test database configured")
	}
	ctx := context.Background()
	mc := &membershipChecker{queries: db.New(testPool)}

	roles := []struct {
		role string
		want bool
	}{
		{"owner", true},
		{"admin", true},
		{"member", true},
		{"super_user", true},
		{"general_user", false},
	}

	for _, tc := range roles {
		t.Run(tc.role, func(t *testing.T) {
			var userID string
			if err := testPool.QueryRow(ctx,
				`INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
				"Realtime Gate "+tc.role, "realtime-gate-"+tc.role+"@multica.test",
			).Scan(&userID); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE id=$1`, userID) })

			if _, err := testPool.Exec(ctx,
				`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3)`,
				testWorkspaceID, userID, tc.role,
			); err != nil {
				t.Fatal(err)
			}

			if !mc.IsMember(ctx, userID, testWorkspaceID) {
				t.Fatalf("IsMember(%s) = false, want true", tc.role)
			}
			if got := mc.CanReceiveWorkspaceEvents(ctx, userID, testWorkspaceID); got != tc.want {
				t.Fatalf("CanReceiveWorkspaceEvents(%s) = %v, want %v", tc.role, got, tc.want)
			}
		})
	}
}
