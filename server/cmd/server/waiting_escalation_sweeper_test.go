package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// setupWaitingEscalationFixture creates an issue in the given status whose
// metadata is a NEX-1043 waiting_on=agent:<agentID> wait, with waiting_since
// artificially set `ageHours` in the past — the same "manufacture an old
// enough row" technique the rest of this file's sibling tests
// (runtime_sweeper_test.go) use for their own SLA windows.
func setupWaitingEscalationFixture(t *testing.T, status string, ageHours int) (issueID, agentID string) {
	t.Helper()
	ctx := context.Background()

	err := testPool.QueryRow(ctx, `
		SELECT a.id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1 AND a.archived_at IS NULL AND a.runtime_id IS NOT NULL
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID)
	if err != nil {
		t.Fatalf("failed to find test agent: %v", err)
	}

	metadata := `{"waiting_on": "agent:` + agentID + `", "unblock_condition": "validator confirms the fix", "waiting_since": "` +
		nowMinusHoursRFC3339(ageHours) + `"}`

	err = testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, metadata)
		SELECT $1, 'Waiting escalation test issue', $2, 'high', 'member', m.user_id, 'agent', $3, $4::jsonb
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, status, agentID, metadata).Scan(&issueID)
	if err != nil {
		t.Fatalf("failed to create waiting-escalation test issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})
	return issueID, agentID
}

func nowMinusHoursRFC3339(hours int) string {
	// Computed in SQL (not Go's time.Now) so the fixture's clock always
	// matches whatever clock the sweeper's own `now()` SQL predicates use.
	var out string
	testPool.QueryRow(context.Background(),
		`SELECT to_char((now() - make_interval(hours => $1::int)) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')`, hours,
	).Scan(&out)
	return out
}

func issueMetadataJSON(t *testing.T, issueID string) map[string]any {
	t.Helper()
	var raw []byte
	if err := testPool.QueryRow(context.Background(), `SELECT metadata FROM issue WHERE id = $1`, issueID).Scan(&raw); err != nil {
		t.Fatalf("failed to read issue metadata: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("failed to decode issue metadata: %v", err)
	}
	return out
}

// TestSweepWaitingRecallFiresPast48Hours is the artificial 48h scenario: a
// blocked issue whose waiting_since is 50 hours in the past (past the
// waitingRecallAfterSeconds threshold) with no prior recall gets recalled —
// a system comment mentioning the agent is posted, a task is enqueued for
// that agent, and escalation_recalled_at is stamped so a second sweep tick
// does not resend.
func TestSweepWaitingRecallFiresPast48Hours(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	issueID, agentID := setupWaitingEscalationFixture(t, "blocked", 50)

	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	sweepWaitingRecalls(ctx, queries, taskSvc)

	meta := issueMetadataJSON(t, issueID)
	if _, ok := meta["escalation_recalled_at"]; !ok {
		t.Fatalf("expected escalation_recalled_at to be stamped after recall, metadata: %v", meta)
	}

	var commentCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1 AND content LIKE '%자동 재호출%'`, issueID).Scan(&commentCount)
	if commentCount != 1 {
		t.Fatalf("expected exactly 1 recall comment, got %d", commentCount)
	}

	var taskCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1 AND agent_id = $2`, issueID, agentID).Scan(&taskCount)
	if taskCount != 1 {
		t.Fatalf("expected exactly 1 enqueued recall task for agent %s, got %d", agentID, taskCount)
	}

	// Second tick must not resend: escalation_recalled_at already present
	// excludes the row from SelectIssuesNeedingWaitingRecall.
	sweepWaitingRecalls(ctx, queries, taskSvc)
	testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1 AND content LIKE '%자동 재호출%'`, issueID).Scan(&commentCount)
	if commentCount != 1 {
		t.Fatalf("expected recall to fire exactly once across two sweep ticks, got %d comments", commentCount)
	}
}

// TestSweepWaitingRecallDoesNotFireBeforeThreshold is the negative control:
// a wait only 10 hours old (well under the 48h threshold) must not recall.
func TestSweepWaitingRecallDoesNotFireBeforeThreshold(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	issueID, _ := setupWaitingEscalationFixture(t, "blocked", 10)

	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	sweepWaitingRecalls(ctx, queries, taskSvc)

	meta := issueMetadataJSON(t, issueID)
	if _, ok := meta["escalation_recalled_at"]; ok {
		t.Fatalf("expected no recall before the 48h threshold, metadata: %v", meta)
	}
}

// TestSweepWaitingReassignFiresPast24HoursAfterRecall is the artificial 24h
// scenario: an issue recalled 26 hours ago (past waitingReassignAfterSeconds)
// with no observable change since (updated_at frozen at the recall write)
// gets reassigned to the CEO queue — waiting_on flips to "ceo",
// escalation_recalled_at is removed, and an audit comment is posted.
func TestSweepWaitingReassignFiresPast24HoursAfterRecall(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	issueID, _ := setupWaitingEscalationFixture(t, "blocked", 74)

	// Simulate "a recall already happened 26 hours ago, and nothing has
	// touched the issue since" — set escalation_recalled_at and pin
	// updated_at to the same instant, exactly what the real recall write
	// does via SetIssueMetadataKey's `updated_at = now()`.
	recalledAt := nowMinusHoursRFC3339(26)
	_, err := testPool.Exec(ctx, `
		UPDATE issue SET
			metadata = metadata || jsonb_build_object('escalation_recalled_at', $2::text),
			updated_at = (now() - interval '26 hours')
		WHERE id = $1
	`, issueID, recalledAt)
	if err != nil {
		t.Fatalf("failed to simulate prior recall: %v", err)
	}

	queries := db.New(testPool)
	sweepWaitingReassignments(ctx, queries)

	meta := issueMetadataJSON(t, issueID)
	if meta["waiting_on"] != "ceo" {
		t.Fatalf("expected waiting_on=ceo after reassign, got: %v", meta["waiting_on"])
	}
	if _, stillRecalled := meta["escalation_recalled_at"]; stillRecalled {
		t.Fatalf("expected escalation_recalled_at to be cleared after reassign, metadata: %v", meta)
	}

	var commentCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1 AND content LIKE '%자동 재배정%'`, issueID).Scan(&commentCount)
	if commentCount != 1 {
		t.Fatalf("expected exactly 1 reassign audit comment, got %d", commentCount)
	}
}

// TestSweepWaitingReassignDoesNotFireIfIssueChangedSinceRecall verifies the
// "observable change" guard: if the assignee touched the issue after the
// recall (updated_at advanced), it must not be swept into the CEO queue even
// though the 24h window has elapsed.
func TestSweepWaitingReassignDoesNotFireIfIssueChangedSinceRecall(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()
	issueID, _ := setupWaitingEscalationFixture(t, "blocked", 74)

	recalledAt := nowMinusHoursRFC3339(26)
	_, err := testPool.Exec(ctx, `
		UPDATE issue SET
			metadata = metadata || jsonb_build_object('escalation_recalled_at', $2::text),
			updated_at = (now() - interval '26 hours')
		WHERE id = $1
	`, issueID, recalledAt)
	if err != nil {
		t.Fatalf("failed to simulate prior recall: %v", err)
	}
	// Simulate the assignee touching the issue 20 hours ago (after the
	// recall, well before now) without changing waiting_on.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET updated_at = now() - interval '20 hours' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("failed to simulate a later touch: %v", err)
	}

	queries := db.New(testPool)
	sweepWaitingReassignments(ctx, queries)

	meta := issueMetadataJSON(t, issueID)
	if meta["waiting_on"] == "ceo" {
		t.Fatalf("expected no reassign when the issue was touched after the recall, metadata: %v", meta)
	}
}

// TestSweepWaitingRecallNeverFiresForCeoOwnedWaits is the condition-5 check:
// waiting_on=ceo must never be recalled, no matter how stale waiting_since
// is, and reassignment must never re-fire on an issue already in the CEO
// queue.
func TestSweepWaitingRecallNeverFiresForCeoOwnedWaits(t *testing.T) {
	if testPool == nil {
		t.Skip("no database connection")
	}
	ctx := context.Background()

	var agentID string
	testPool.QueryRow(ctx, `
		SELECT a.id FROM agent a
		JOIN member m ON m.workspace_id = a.workspace_id
		JOIN "user" u ON u.id = m.user_id
		WHERE u.email = $1
		LIMIT 1
	`, integrationTestEmail).Scan(&agentID)

	var issueID string
	err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_type, creator_id, assignee_type, assignee_id, metadata)
		SELECT $1, 'CEO-owned wait test issue', 'blocked', 'high', 'member', m.user_id, 'agent', $2,
			jsonb_build_object('waiting_on', 'ceo', 'unblock_condition', 'CEO approves', 'waiting_since',
				to_char((now() - interval '200 hours') AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'))
		FROM member m WHERE m.workspace_id = $1 LIMIT 1
		RETURNING id
	`, testWorkspaceID, agentID).Scan(&issueID)
	if err != nil {
		t.Fatalf("failed to create ceo-owned test issue: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID)
	})

	queries := db.New(testPool)
	bus := events.New()
	taskSvc := service.NewTaskService(queries, testPool, nil, bus)

	sweepWaitingRecalls(ctx, queries, taskSvc)
	sweepWaitingReassignments(ctx, queries)

	var commentCount int
	testPool.QueryRow(ctx, `SELECT count(*) FROM comment WHERE issue_id = $1`, issueID).Scan(&commentCount)
	if commentCount != 0 {
		t.Fatalf("expected zero automated comments on a ceo-owned wait, got %d", commentCount)
	}
	meta := issueMetadataJSON(t, issueID)
	if _, recalled := meta["escalation_recalled_at"]; recalled {
		t.Fatalf("expected a ceo-owned wait to never be recalled, metadata: %v", meta)
	}
}
