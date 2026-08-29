package main

import (
	"context"
	"net/http"
	"testing"
)

// TestGeneralUserCannotReadWorkspaceDirectoryRoutes exercises the real router
// rather than handlers directly. The workspace directory lives outside the
// generic workspace-scoped route group, so it must carry the same general_user
// restriction explicitly.
func TestGeneralUserCannotReadWorkspaceDirectoryRoutes(t *testing.T) {
	ctx := context.Background()
	var generalUserID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO "user" (name, email) VALUES ('Router General User', 'router-general-user@multica.test') RETURNING id`,
	).Scan(&generalUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx,
		`INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'general_user')`, testWorkspaceID, generalUserID,
	); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE id=$1`, generalUserID) })

	generalToken, err := generateTestJWT(generalUserID, "router-general-user@multica.test", "Router General User")
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{
		"/api/workspaces/",
		"/api/workspaces/" + testWorkspaceID + "/",
		"/api/workspaces/" + testWorkspaceID + "/members",
		"/api/workspaces/" + testWorkspaceID + "/invitations",
	}

	for _, path := range paths {
		t.Run("general forbidden "+path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, testServer.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+generalToken)
			req.Header.Set("X-Workspace-ID", testWorkspaceID)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("GET %s: want 403, got %d", path, resp.StatusCode)
			}
		})

		t.Run("legacy member allowed "+path, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, testServer.URL+path, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+testToken)
			req.Header.Set("X-Workspace-ID", testWorkspaceID)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("GET %s as legacy member: want 200, got %d", path, resp.StatusCode)
			}
		})
	}

	if err := testPool.QueryRow(ctx, `SELECT role FROM member WHERE workspace_id=$1 AND user_id=$2`, testWorkspaceID, generalUserID).Scan(new(string)); err != nil {
		t.Fatalf("general fixture membership changed: %v", err)
	}
	if err := testPool.QueryRow(ctx, `SELECT role FROM member WHERE workspace_id=$1 AND user_id=$2`, testWorkspaceID, testUserID).Scan(new(string)); err != nil {
		t.Fatalf("legacy fixture membership changed: %v", err)
	}
}
