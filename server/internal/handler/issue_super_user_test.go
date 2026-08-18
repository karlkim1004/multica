package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestSuperUserCannotMutateAnotherMembersIssue is the first RBAC tracer
// bullet: a forbidden request must leave the persisted issue unchanged.
func TestSuperUserCannotMutateAnotherMembersIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("PostgreSQL test database unavailable")
	}
	ctx := context.Background()
	var userID, issueID, title string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name,email) VALUES ('super user','super-user-rbac@multica.test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE id=$1`, userID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'super_user')`, testWorkspaceID, userID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,position,number) VALUES ($1,'owner issue','todo','none','member',$2,0,999999) RETURNING id,title`, testWorkspaceID, testUserID).Scan(&issueID, &title); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id=$1`, issueID) })
	r := newRequest(http.MethodPut, "/api/issues/"+issueID+"?workspace_id="+testWorkspaceID, map[string]any{"title": "mutated"})
	r.Header.Set("X-User-ID", userID)
	r = withURLParam(r, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.UpdateIssue(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var got string
	if err := testPool.QueryRow(ctx, `SELECT title FROM issue WHERE id=$1`, issueID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != title {
		t.Fatalf("issue changed: got %q want %q", got, title)
	}
}

func TestSuperUserCannotDeleteAnotherMembersIssue(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("PostgreSQL test database unavailable")
	}
	ctx := context.Background()
	var userID, issueID string
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name,email) VALUES ('super delete','super-delete-rbac@multica.test') RETURNING id`).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE id=$1`, userID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'super_user')`, testWorkspaceID, userID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,position,number) VALUES ($1,'delete owner issue','todo','none','member',$2,0,999998) RETURNING id`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM issue WHERE id=$1`, issueID) })
	r := newRequest(http.MethodDelete, "/api/issues/"+issueID+"?workspace_id="+testWorkspaceID, nil)
	r.Header.Set("X-User-ID", userID)
	r = withURLParam(r, "id", issueID)
	w := httptest.NewRecorder()
	testHandler.DeleteIssue(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var remaining int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE id=$1`, issueID).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 1 {
		t.Fatalf("issue deleted after forbidden request: remaining=%d", remaining)
	}
}
