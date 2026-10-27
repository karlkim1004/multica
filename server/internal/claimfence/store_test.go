package claimfence

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func isolatedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("NEX1499_TEST_DB_URL")
	if dsn == "" {
		t.Skip("NEX1499_TEST_DB_URL is required for isolated PostgreSQL tests")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "127.0.0.1" || u.Path != "/nex1499_isolated" {
		t.Fatal("NEX1499_TEST_DB_URL must point to the isolated loopback database")
	}
	admin, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "nex1499_" + uuid.New().String()[:8]
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(context.Background(), "CREATE SCHEMA "+quoted); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Errorf("remove isolated schema: %v", err)
		}
		admin.Close()
	})
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	appName := "nex1499_" + uuid.New().String()[:8]
	config.ConnConfig.RuntimeParams["application_name"] = appName
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	_, err = pool.Exec(context.Background(), `
		CREATE TABLE claim_fence (
		 runtime_id uuid PRIMARY KEY, owner_id uuid, epoch bigint NOT NULL DEFAULT 0
		);
		CREATE TABLE claim_receipt (
		 runtime_id uuid NOT NULL, operation_id uuid NOT NULL, task_id uuid,
		 PRIMARY KEY (runtime_id, operation_id)
		);
		CREATE TABLE claim_test_task (id uuid PRIMARY KEY, status text NOT NULL);
	`)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("NEX1499_TEST_APP_NAME", appName)
	return pool
}

func TestFenceWaitsForClaimTransactionAndRecoversItsReceipt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool := isolatedPool(t)
	store := New(pool)
	runtimeID, operationID, taskID, ownerID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	entered, finish := make(chan struct{}), make(chan struct{})
	claimDone := make(chan error, 1)
	go func() {
		_, err := store.Claim(ctx, runtimeID, operationID, func(ctx context.Context, tx pgx.Tx) (*uuid.UUID, error) {
			var isolation string
			if err := tx.QueryRow(ctx, `SHOW transaction_isolation`).Scan(&isolation); err != nil {
				return nil, err
			}
			if isolation != "read committed" {
				return nil, fmt.Errorf("transaction isolation = %q, want read committed", isolation)
			}
			close(entered)
			select {
			case <-finish:
				return &taskID, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})
		claimDone <- err
	}()
	select {
	case <-entered:
	case err := <-claimDone:
		t.Fatalf("claim failed before assignment: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	fenceStarted := make(chan struct{})
	fenceDone := make(chan error, 1)
	go func() {
		close(fenceStarted)
		_, err := store.Acquire(ctx, runtimeID, ownerID)
		fenceDone <- err
	}()
	<-fenceStarted
	if err := waitForLockWait(ctx, pool); err != nil {
		close(finish)
		<-claimDone
		t.Fatal(err)
	}
	close(finish)
	if err := <-claimDone; err != nil {
		t.Fatal(err)
	}
	if err := <-fenceDone; err != nil {
		t.Fatal(err)
	}
	receipt, err := store.Claim(ctx, runtimeID, operationID, func(context.Context, pgx.Tx) (*uuid.UUID, error) {
		t.Fatal("reconciliation must not assign twice")
		return nil, nil
	})
	if err != nil || receipt == nil || *receipt != taskID {
		t.Fatalf("receipt = %v, %v; want %s", receipt, err, taskID)
	}
	t.Logf("runtime=%s operation=%s task=%s postgres-wait_event=Lock claim-commit=before-fence receipt=recovered", runtimeID, operationID, taskID)
}

func waitForLockWait(ctx context.Context, pool *pgxpool.Pool) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err := pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_stat_activity
			WHERE application_name=$1 AND wait_event_type='Lock'
		)`, os.Getenv("NEX1499_TEST_APP_NAME")).Scan(&waiting)
		if err != nil {
			return fmt.Errorf("observe PostgreSQL lock wait: %w", err)
		}
		if waiting {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("no PostgreSQL lock wait observed: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestFailedAssignmentRollsBackAndOtherOwnerCannotAcquire(t *testing.T) {
	ctx := context.Background()
	pool := isolatedPool(t)
	store := New(pool)
	runtimeID, operationID, taskID, ownerID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO claim_test_task VALUES ($1, 'queued')`, taskID); err != nil {
		t.Fatal(err)
	}
	failed := errors.New("injected failure")
	_, err := store.Claim(ctx, runtimeID, operationID, func(ctx context.Context, tx pgx.Tx) (*uuid.UUID, error) {
		if _, err := tx.Exec(ctx, `UPDATE claim_test_task SET status='dispatched' WHERE id=$1`, taskID); err != nil {
			return nil, err
		}
		return nil, failed
	})
	if !errors.Is(err, failed) {
		t.Fatalf("claim error = %v", err)
	}
	var status string
	if err := pool.QueryRow(ctx, `SELECT status FROM claim_test_task WHERE id=$1`, taskID).Scan(&status); err != nil || status != "queued" {
		t.Fatalf("rolled back status = %s, %v", status, err)
	}
	epoch, err := store.Acquire(ctx, runtimeID, ownerID)
	if err != nil || epoch != 1 {
		t.Fatalf("acquire = %d, %v", epoch, err)
	}
	again, err := store.Acquire(ctx, runtimeID, ownerID)
	if err != nil || again != epoch {
		t.Fatalf("repeat acquire = %d, %v", again, err)
	}
	if _, err = store.Acquire(ctx, runtimeID, uuid.New()); !errors.Is(err, ErrFenceOwned) {
		t.Fatalf("other owner: %v", err)
	}
	if _, err = store.Claim(ctx, runtimeID, operationID, func(context.Context, pgx.Tx) (*uuid.UUID, error) { t.Fatal("assignment under fence"); return nil, nil }); !errors.Is(err, ErrFenced) {
		t.Fatalf("failed operation while fenced = %v", err)
	}
	t.Logf("runtime=%s operation=%s task=%s rollback=queued epoch=%d other-owner=blocked", runtimeID, operationID, taskID, epoch)
}

func TestLostResponseCanBeReconciledAfterFence(t *testing.T) {
	ctx := context.Background()
	pool := isolatedPool(t)
	runtimeID, operationID, taskID, ownerID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO claim_test_task VALUES ($1, 'queued')`, taskID); err != nil {
		t.Fatal(err)
	}
	store := New(pool)
	claim := func(ctx context.Context, tx pgx.Tx) (*uuid.UUID, error) {
		var id uuid.UUID
		err := tx.QueryRow(ctx, `UPDATE claim_test_task SET status = 'dispatched' WHERE id = $1 AND status = 'queued' RETURNING id`, taskID).Scan(&id)
		return &id, err
	}
	// Discard the first successful response, as if it was lost in transit.
	if _, err := store.Claim(ctx, runtimeID, operationID, claim); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(ctx, runtimeID, ownerID); err != nil {
		t.Fatal(err)
	}
	store = New(pool) // A new controller instance reads the durable receipt.
	receipt, err := store.Claim(ctx, runtimeID, operationID, claim)
	if err != nil || receipt == nil || *receipt != taskID {
		t.Fatalf("reconcile = %v, %v; want task %s", receipt, err, taskID)
	}
	if _, err := store.Claim(ctx, runtimeID, uuid.New(), claim); !errors.Is(err, ErrFenced) {
		t.Fatalf("new claim while fenced = %v; want ErrFenced", err)
	}
	t.Logf("runtime=%s operation=%s task=%s fence=closed reconciled=same-task new-claim=blocked", runtimeID, operationID, taskID)
}
