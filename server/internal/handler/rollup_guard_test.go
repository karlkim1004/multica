package handler

import (
	"context"
	"testing"
)

// rollupSingletonTestLock serialises tests in separate package binaries that
// share task_usage_hourly_rollup_state(id=1) and rollup advisory lock 4246.
// It is deliberately distinct from the production function lock.
const rollupSingletonTestLock int64 = 42463980

func lockRollupSingleton(t *testing.T) {
	t.Helper()
	conn, err := testPool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire rollup-guard connection: %v", err)
	}
	if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_lock($1)`, rollupSingletonTestLock); err != nil {
		conn.Release()
		t.Fatalf("acquire rollup singleton guard: %v", err)
	}
	t.Cleanup(func() {
		if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, rollupSingletonTestLock); err != nil {
			t.Logf("release rollup singleton guard: %v", err)
		}
		conn.Release()
	})
}
