package handler

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func poolUUID(t *testing.T) pgtype.UUID {
	t.Helper()
	var id pgtype.UUID
	if err := testPool.QueryRow(context.Background(), "SELECT gen_random_uuid()").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func poolHolders(t *testing.T, workspace pgtype.UUID) (pgtype.UUID, pgtype.UUID) {
	t.Helper()
	ctx := context.Background()
	var first, runtime, second pgtype.UUID
	if err := testPool.QueryRow(ctx, "SELECT id, runtime_id FROM agent WHERE workspace_id=$1 AND runtime_id IS NOT NULL LIMIT 1", workspace).Scan(&first, &runtime); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO agent (workspace_id,name,runtime_mode,runtime_config,runtime_id) VALUES ($1,'pool-lock-holder-' || gen_random_uuid(),'local','{}',$2) RETURNING id`, workspace, runtime).Scan(&second); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, "DELETE FROM worker_pool_lock WHERE holder_id=$1", second)
		_, _ = testPool.Exec(ctx, "DELETE FROM agent WHERE id=$1", second)
	})
	return first, second
}

// TestWorkerPoolLocksRace exercises two real database connections released at
// the same instant. Exactly one UPSERT may acquire each contested resource.
func TestWorkerPoolLocksRace(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	p := service.NewWorkerPoolService(q)
	workspace, issue := pgtype.UUID{}, poolUUID(t)
	if err := workspace.Scan(testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	first, second := poolHolders(t, workspace)
	for _, tc := range []struct{ name, file string }{{"issue", ""}, {"file", "cmd/server/main.go"}, {"deploy", "production"}} {
		t.Run(tc.name, func(t *testing.T) {
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for _, holder := range []pgtype.UUID{first, second} {
				wg.Add(1)
				go func(holder pgtype.UUID) {
					defer wg.Done()
					<-start
					if tc.name == "issue" {
						results <- p.AcquireIssueLock(ctx, workspace, issue, holder)
					} else if tc.name == "deploy" {
						results <- p.AcquireDeployLock(ctx, workspace, tc.file, holder)
					} else {
						results <- p.AcquireFileLock(ctx, workspace, tc.file, holder)
					}
				}(holder)
			}
			close(start)
			wg.Wait()
			close(results)
			wins := 0
			for err := range results {
				if err == nil {
					wins++
				} else if err != service.ErrWorkerPoolLockHeld {
					t.Fatalf("unexpected acquire error: %v", err)
				}
			}
			if wins != 1 {
				t.Fatalf("concurrent %s lock winners=%d, want 1", tc.name, wins)
			}
		})
	}
}

// TestPoolDispatchOverloadClonesAndEnqueues proves the production path rather
// than a helper API: no idle worker -> compatible online runtime selected ->
// credential-safe clone -> atomic issue claim -> normal task queue enqueue.
func TestPoolDispatchOverloadClonesAndEnqueues(t *testing.T) {
	ctx := context.Background()
	var workspace, source, sourceRuntime, target pgtype.UUID
	if err := workspace.Scan(testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, "SELECT id, runtime_id FROM agent WHERE workspace_id=$1 AND runtime_id IS NOT NULL LIMIT 1", workspace).Scan(&source, &sourceRuntime); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, "UPDATE agent SET status='working', max_concurrent_tasks=1, runtime_config='{\"token\":\"source-only\"}', custom_env='{\"secret\":\"source-only\"}' WHERE workspace_id=$1", workspace); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, "UPDATE agent SET status='idle', max_concurrent_tasks=1 WHERE workspace_id=$1", workspace)
	})
	if err := testPool.QueryRow(ctx, `INSERT INTO agent_runtime (workspace_id,name,runtime_mode,provider,status,device_info) SELECT workspace_id,'overload target ' || gen_random_uuid(),runtime_mode,provider,'online','test' FROM agent_runtime WHERE id=$1 RETURNING id`, sourceRuntime).Scan(&target); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, "DELETE FROM agent_runtime WHERE id=$1", target) })
	// This assigned issue creates the source's active task, making it overloaded.
	createIssueForTest(t, map[string]any{"title": "overload source task", "status": "todo", "assignee_type": "agent", "assignee_id": source.String()})
	issue := createIssueForTest(t, map[string]any{"title": "overload clone dispatch", "status": "todo", "pool_dispatch": true})
	var cloneID, runtimeID string
	var runtimeConfig, customEnv []byte
	if err := testPool.QueryRow(ctx, `SELECT a.id, a.runtime_id, a.runtime_config, a.custom_env FROM issue i JOIN agent a ON a.id=i.assignee_id WHERE i.id=$1`, issue.ID).Scan(&cloneID, &runtimeID, &runtimeConfig, &customEnv); err != nil {
		t.Fatalf("load clone assignment: %v", err)
	}
	if runtimeID != target.String() || string(runtimeConfig) != "{}" || string(customEnv) != "{}" {
		t.Fatalf("wrong target or credential leak: runtime=%s config=%s env=%s", runtimeID, runtimeConfig, customEnv)
	}
	if taskCountFor(t, issue.ID, cloneID) != 1 {
		t.Fatalf("clone did not receive exactly one queued task")
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, "DELETE FROM agent WHERE id=$1", cloneID) })
}

func TestWorkerPoolLockExpires(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	p := service.NewWorkerPoolService(q)
	var workspace pgtype.UUID
	if err := workspace.Scan(testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	issue := poolUUID(t)
	first, second := poolHolders(t, workspace)
	if err := p.AcquireIssueLock(ctx, workspace, issue, first); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(ctx, "UPDATE worker_pool_lock SET expires_at = now() - interval '1 second' WHERE workspace_id=$1 AND resource_type='issue' AND resource_key=$2", workspace, issue.String()); err != nil {
		t.Fatal(err)
	}
	if err := p.AcquireIssueLock(ctx, workspace, issue, second); err != nil {
		t.Fatalf("expired lock was not reclaimed: %v", err)
	}
}

func TestClonePersonaExcludesCredentials(t *testing.T) {
	ctx := context.Background()
	q := db.New(testPool)
	p := service.NewWorkerPoolService(q)
	var workspace, source, sourceRuntime, target pgtype.UUID
	if err := workspace.Scan(testWorkspaceID); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, "SELECT id, runtime_id FROM agent WHERE workspace_id=$1 AND runtime_id IS NOT NULL LIMIT 1", workspace).Scan(&source, &sourceRuntime); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `INSERT INTO agent_runtime (workspace_id,name,runtime_mode,provider,status,device_info) SELECT workspace_id,'pool clone target ' || gen_random_uuid(),runtime_mode,provider,'online','test' FROM agent_runtime WHERE id=$1 RETURNING id`, sourceRuntime).Scan(&target); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, "DELETE FROM agent_runtime WHERE id=$1", target) })
	if _, err := testPool.Exec(ctx, "UPDATE agent SET instructions='portable persona instruction', custom_env='{\"secret\":\"no-copy\"}', runtime_config='{\"token\":\"no-copy\"}', custom_args='[\"--secret\"]', mcp_config='{\"token\":\"no-copy\"}' WHERE id=$1", source); err != nil {
		t.Fatal(err)
	}
	clone, err := p.ClonePersonaToRuntime(ctx, source, workspace, target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, "DELETE FROM agent WHERE id=$1", clone.ID) })
	if clone.RuntimeID != target || string(clone.RuntimeConfig) != "{}" || string(clone.CustomEnv) != "{}" || string(clone.CustomArgs) != "[]" || clone.McpConfig != nil {
		t.Fatalf("credential/runtime state leaked into clone: %+v", clone)
	}
	if clone.Instructions != "portable persona instruction" {
		t.Fatal("portable persona fields were not preserved")
	}
}
