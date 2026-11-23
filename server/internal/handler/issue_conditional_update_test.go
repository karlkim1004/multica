package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUpdateIssueConditionalRace(t *testing.T) {
	issueID := createMetadataTestIssue(t, "conditional issue update "+time.Now().Format(time.RFC3339Nano))
	update := func(body map[string]any) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := withURLParam(newRequest(http.MethodPut, "/api/issues/"+issueID+"/conditional-update", body), "id", issueID)
		testHandler.UpdateIssueConditional(w, r)
		return w
	}
	read := func() IssueResponse {
		w := httptest.NewRecorder()
		testHandler.GetIssue(w, withURLParam(newRequest(http.MethodGet, "/api/issues/"+issueID, nil), "id", issueID))
		if w.Code != http.StatusOK {
			t.Fatalf("get issue: %d %s", w.Code, w.Body)
		}
		var issue IssueResponse
		if err := json.Unmarshal(w.Body.Bytes(), &issue); err != nil {
			t.Fatal(err)
		}
		return issue
	}

	initial := read()
	if w := update(map[string]any{"title": "missing version"}); w.Code != http.StatusBadRequest {
		t.Fatalf("missing version: %d %s", w.Code, w.Body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lock, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback(context.Background())
	if _, err := lock.Exec(ctx, "SELECT id FROM issue WHERE id = $1 FOR UPDATE", issueID); err != nil {
		t.Fatal(err)
	}

	candidates := []map[string]any{
		{"status": "blocked", "assignee_type": "member", "assignee_id": testUserID,
			"metadata_patch": map[string]any{"waiting_on": "ceo", "unblock_condition": "Approval recorded",
				"waiting_since": initial.UpdatedAt, "blocked_candidate": true}},
		{"status": "in_review", "assignee_type": nil, "assignee_id": nil,
			"metadata_patch": map[string]any{"review_candidate": true}},
	}
	type result struct {
		index int
		w     *httptest.ResponseRecorder
	}
	results := make(chan result, len(candidates))
	for i, candidate := range candidates {
		candidate["expected_updated_at"] = initial.UpdatedAt
		go func(i int, candidate map[string]any) { results <- result{i, update(candidate)} }(i, candidate)
	}

	// Both requests must reach the SQL update before the lock is released.
	// Otherwise a second request could fail in the handler's pre-check alone.
	for {
		var blocked int
		err := testPool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'
			  AND query LIKE '-- name: UpdateIssue :one%'`).Scan(&blocked)
		if err != nil {
			t.Fatalf("wait for competing SQL updates: %v", err)
		}
		if blocked == len(candidates) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := lock.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	var winner, loser = -1, -1
	for range candidates {
		select {
		case result := <-results:
			switch result.w.Code {
			case http.StatusOK:
				winner = result.index
			case http.StatusConflict:
				loser = result.index
			default:
				t.Fatalf("candidate %d: %d %s", result.index, result.w.Code, result.w.Body)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if winner < 0 || loser < 0 {
		t.Fatalf("want one 200 and one 409, winner=%d loser=%d", winner, loser)
	}

	final := read()
	if final.Status != candidates[winner]["status"] || final.UpdatedAt == initial.UpdatedAt {
		t.Fatalf("unexpected persisted issue: %+v", final)
	}
	winningValues := candidates[winner]["metadata_patch"].(map[string]any)
	losingValues := candidates[loser]["metadata_patch"].(map[string]any)
	for key, value := range winningValues {
		if final.Metadata[key] != value {
			t.Errorf("winning metadata %s: want %v, got %v", key, value, final.Metadata[key])
		}
	}
	for key := range losingValues {
		if _, exists := final.Metadata[key]; exists {
			t.Errorf("losing metadata leaked: %s", key)
		}
	}
	if _, hasWarning := final.Metadata[waitingMetadataWarningKey]; hasWarning != (winner == 1) {
		t.Errorf("unexpected waiting metadata warning for winner %d: %v", winner, final.Metadata)
	}
	if winner == 0 && (final.AssigneeID == nil || *final.AssigneeID != testUserID) ||
		winner == 1 && final.AssigneeID != nil {
		t.Errorf("unexpected assignment: %v", final.AssigneeID)
	}
	if w := update(candidates[loser]); w.Code != http.StatusConflict {
		t.Fatalf("retry with stale version: %d %s", w.Code, w.Body)
	}
	if afterRetry := read(); afterRetry.UpdatedAt != final.UpdatedAt {
		t.Fatalf("stale retry changed version: %s -> %s", final.UpdatedAt, afterRetry.UpdatedAt)
	}
	candidates[loser]["expected_updated_at"] = final.UpdatedAt
	if w := update(candidates[loser]); w.Code != http.StatusOK {
		t.Fatalf("re-evaluated update with current version: %d %s", w.Code, w.Body)
	}
	t.Logf("concurrent 200/409 for version %s; stale retry 409; current-version retry 200", initial.UpdatedAt)
}
