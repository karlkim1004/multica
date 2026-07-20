package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestConcurrentMembershipApplicationApprovalCreatesOneMembership(t *testing.T) {
	ctx := t.Context()
	var applicantID, applicationID string
	email := fmt.Sprintf("application-applicant-%d@multica.ai", time.Now().UnixNano())
	if err := testPool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ('Application Applicant', $1) RETURNING id`, email).Scan(&applicantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE id = $1`, applicantID) })
	if err := testPool.QueryRow(ctx, `INSERT INTO workspace_membership_application (workspace_id, user_id) VALUES ($1, $2) RETURNING id`, testWorkspaceID, applicantID).Scan(&applicationID); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := newRequest(http.MethodPost, "/", nil)
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("id", testWorkspaceID)
			rctx.URLParams.Add("applicationId", applicationID)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			rr := httptest.NewRecorder()
			testHandler.ApproveMembershipApplication(rr, req)
			statuses <- rr.Code
		}()
	}
	close(start)
	wg.Wait()
	close(statuses)
	for status := range statuses {
		if status != http.StatusOK && status != http.StatusConflict {
			t.Fatalf("approve status = %d, want 200 or 409", status)
		}
	}
	var memberships int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, applicantID).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if memberships != 1 {
		t.Fatalf("memberships = %d, want 1", memberships)
	}
}
