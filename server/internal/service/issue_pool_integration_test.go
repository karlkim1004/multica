package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// Fixtures live in one rolled-back transaction and require an explicitly
// configured test database; never infer a developer's normal database URL.
func TestPoolAuthRecoverySQL(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is required for database integration tests")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var workspace, otherWorkspace pgtype.UUID
	for _, dest := range []*pgtype.UUID{&workspace, &otherWorkspace} {
		if err := tx.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('auth recovery test', gen_random_uuid()::text) RETURNING id`).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	var runtime, otherRuntime pgtype.UUID
	for _, dest := range []*pgtype.UUID{&runtime, &otherRuntime} {
		if err := tx.QueryRow(ctx, `INSERT INTO agent_runtime (workspace_id,name,runtime_mode,provider) VALUES ($1,'test','local',gen_random_uuid()::text) RETURNING id`, workspace).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	var agentID pgtype.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO agent (workspace_id,name,runtime_mode,runtime_id) VALUES ($1,'auth test','local',$2) RETURNING id`, workspace, runtime).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	agent := db.Agent{ID: agentID, RuntimeID: runtime}
	q := db.New(tx)
	base := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	insert := func(status, reason, message string, runtimeID pgtype.UUID, created, completed int) {
		t.Helper()
		_, err := tx.Exec(ctx, `INSERT INTO agent_task_queue (agent_id,runtime_id,status,failure_reason,error,created_at,completed_at) VALUES ($1,$2,$3,$4,$5,$6,$7)`, agentID, runtimeID, status, reason, message, base.Add(time.Duration(created)*time.Minute), base.Add(time.Duration(completed)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
	}
	check := func(name string, ws, rt pgtype.UUID, count int, blocked bool) {
		t.Helper()
		rows, err := q.ListAgentRuntimeFailuresSinceSuccess(ctx, db.ListAgentRuntimeFailuresSinceSuccessParams{WorkspaceID: ws, AgentID: agentID, RuntimeID: rt})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		current := agent
		current.RuntimeID = rt
		if len(rows) != count || poolFailuresBlockRecovery(rows, current) != blocked {
			t.Fatalf("%s: failures=%d blocked=%v, want %d/%v", name, len(rows), poolFailuresBlockRecovery(rows, current), count, blocked)
		}
	}
	insert("completed", "", "", runtime, 0, 1)
	insert("failed", taskfailure.ReasonAgentUnknown.String(), "OAuth token has expired. Please obtain a new token or refresh your existing token.", runtime, 2, 3)
	insert("failed", taskfailure.ReasonAgentProviderNetwork.String(), "connection refused", runtime, 4, 5)
	insert("cancelled", "", "", runtime, 6, 7)
	check("network and cancellation do not clear auth", workspace, runtime, 2, true)
	check("workspace isolation", otherWorkspace, runtime, 0, false)
	check("new runtime can recover", workspace, otherRuntime, 0, false)
	insert("completed", "", "", otherRuntime, 8, 9)
	check("other runtime success does not clear original", workspace, runtime, 2, true)
	// A task started before the failures but completed after them is the newest
	// recovery evidence: created_at ordering would incorrectly keep it blocked.
	insert("completed", "", "", runtime, 0, 10)
	check("later completion clears auth", workspace, runtime, 0, false)
	insert("failed", taskfailure.ReasonAgentProviderAuthOrAccess.String(), "credentials rejected", runtime, 1, 11)
	check("late-finishing auth failure blocks again", workspace, runtime, 1, true)
}

func TestPoolCompletedTaskSuppressesStaleRequeueUntilIssueChanges(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL is required for database integration tests")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)

	var workspace, runtimeID, agentID, completedIssueID, pendingIssueID, reactivatedIssueID pgtype.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO workspace (name, slug) VALUES ('pool completion test', gen_random_uuid()::text) RETURNING id`).Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO agent_runtime (workspace_id,name,runtime_mode,provider) VALUES ($1,'pool completion test','local',gen_random_uuid()::text) RETURNING id`, workspace).Scan(&runtimeID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO agent (workspace_id,name,runtime_mode,runtime_id) VALUES ($1,'pool completion test','local',$2) RETURNING id`, workspace, runtimeID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	createIssue := func(title string, number int32) pgtype.UUID {
		t.Helper()
		var issueID pgtype.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO issue (workspace_id,title,status,priority,creator_type,creator_id,assignee_type,assignee_id,number) VALUES ($1,$2,'todo','none','agent',$3,'agent',$3,$4) RETURNING id`, workspace, title, agentID, number).Scan(&issueID); err != nil {
			t.Fatal(err)
		}
		return issueID
	}
	completedIssueID = createIssue("completed task wait", 1)
	pendingIssueID = createIssue("unstarted stale issue", 2)
	reactivatedIssueID = createIssue("issue updated after old task", 3)
	updatedAt := time.Now().UTC().Add(-7 * time.Hour)
	if _, err := tx.Exec(ctx, `UPDATE issue SET updated_at=$2 WHERE id IN ($1,$3,$4)`, completedIssueID, updatedAt, pendingIssueID, reactivatedIssueID); err != nil {
		t.Fatal(err)
	}
	completedAt := time.Now().UTC().Add(-6 * time.Hour)
	if _, err := tx.Exec(ctx, `INSERT INTO agent_task_queue (agent_id,runtime_id,issue_id,status,priority,started_at,completed_at) VALUES ($1,$2,$3,'completed',0,$4,$5)`, agentID, runtimeID, completedIssueID, completedAt.Add(-time.Minute), completedAt); err != nil {
		t.Fatal(err)
	}
	oldCompletion := updatedAt.Add(-time.Hour)
	if _, err := tx.Exec(ctx, `INSERT INTO agent_task_queue (agent_id,runtime_id,issue_id,status,priority,started_at,completed_at) VALUES ($1,$2,$3,'completed',0,$4,$5)`, agentID, runtimeID, reactivatedIssueID, oldCompletion.Add(-time.Minute), oldCompletion); err != nil {
		t.Fatal(err)
	}
	q := db.New(tx)
	if _, err := tx.Exec(ctx, `UPDATE agent SET status='idle' WHERE id=$1`, agentID); err != nil {
		t.Fatal(err)
	}
	bus := events.New()
	taskService := NewTaskService(q, tx, nil, bus)
	issueService := NewIssueService(q, tx, bus, nil, taskService)
	issueService.SweepStaleAssigned(ctx, workspace)
	for _, tc := range []struct {
		name    string
		issueID pgtype.UUID
		want    int
	}{
		{"completed task suppresses repeat", completedIssueID, 1},
		{"never-started stale issue is dispatched", pendingIssueID, 1},
		{"issue update after old task dispatches", reactivatedIssueID, 2},
	} {
		var count int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, tc.issueID).Scan(&count); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if count != tc.want {
			t.Errorf("%s: task count=%d, want %d", tc.name, count, tc.want)
		}
	}
}
