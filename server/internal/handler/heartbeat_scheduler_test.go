package handler

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// TestBatchedHeartbeatScheduler_CoalescesAndFlushes confirms the core P1 win:
// many Schedule calls for the same id within a tick window collapse to a
// single bulk UPDATE, and the DB observes the bump after FlushNow.
func TestBatchedHeartbeatScheduler_CoalescesAndFlushes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)

	// Push the row's last_seen_at into the past so the post-flush value is
	// distinguishable from the pre-flush one.
	stale := time.Now().Add(-2 * time.Hour)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0, testHandler.Bus)

	// Hammer Schedule with the same id from many goroutines.
	const callers = 50
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			if err := sched.Schedule(context.Background(), rt); err != nil {
				t.Errorf("Schedule: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := sched.PendingCount(); got != 1 {
		t.Fatalf("expected coalesced pending=1, got %d", got)
	}

	// Pre-flush the DB row should still show the stale value.
	_, lastSeenBefore, _ := readRuntimeRow(t, runtimeID)
	if !lastSeenBefore.Equal(stale) {
		// stale time is rounded by the DB, allow same instant
		if lastSeenBefore.After(stale.Add(time.Second)) {
			t.Fatalf("DB unexpectedly bumped before flush: %s", lastSeenBefore)
		}
	}

	sched.FlushNow(context.Background())

	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("expected pending=0 after flush, got %d", got)
	}

	_, lastSeenAfter, _ := readRuntimeRow(t, runtimeID)
	if !lastSeenAfter.After(stale.Add(time.Hour)) {
		t.Fatalf("flush did not bump last_seen_at: stale=%s after=%s", stale, lastSeenAfter)
	}
}

// TestBatchedHeartbeatScheduler_OfflineFallsBackSync confirms that the sync
// path is preserved: an offline-status row goes through MarkAgentRuntimeOnline
// immediately, not through the queue.
func TestBatchedHeartbeatScheduler_OfflineFallsBackSync(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	setRuntimeStatus(t, runtimeID, "offline")
	setRuntimeLastSeenAt(t, runtimeID, time.Now())
	rt := loadRuntime(t, runtimeID)
	if rt.Status != "offline" {
		t.Fatalf("setup: status=%q want offline", rt.Status)
	}

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0, testHandler.Bus)
	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("offline row should not have been queued, pending=%d", got)
	}
	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "online" {
		t.Fatalf("expected status=online after sync flip, got %q", status)
	}
}

// TestBatchedHeartbeatScheduler_StopDrains confirms the shutdown contract:
// IDs queued before Stop must be flushed to the DB by the time Stop returns,
// otherwise a graceful restart would lose heartbeat state.
func TestBatchedHeartbeatScheduler_StopDrains(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	stale := time.Now().Add(-2 * time.Hour)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	// Long tick so the natural ticker can't fire during the test — only
	// the Stop drain can flush.
	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, time.Hour, testHandler.Bus)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Run(ctx)

	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if got := sched.PendingCount(); got != 1 {
		t.Fatalf("expected pending=1 before Stop, got %d", got)
	}

	sched.Stop()

	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("expected pending=0 after Stop drain, got %d", got)
	}
	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(stale.Add(time.Hour)) {
		t.Fatalf("Stop did not drain pending bump: stale=%s after=%s", stale, lastSeen)
	}
}

// TestBatchedHeartbeatScheduler_StopFlushesLateSchedule verifies the
// defense-in-depth flush in Stop(): if Run already returned via ctx.Done()
// and a heartbeat is then Schedule'd before Stop is called, that bump must
// still hit the DB after Stop returns. This guards the production shutdown
// race where in-flight HTTP heartbeats can call Schedule while sweepCtx is
// already cancelled.
func TestBatchedHeartbeatScheduler_StopFlushesLateSchedule(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	stale := time.Now().Add(-2 * time.Hour)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, time.Hour, testHandler.Bus)

	runCtx, runCancel := context.WithCancel(context.Background())
	go sched.Run(runCtx)

	// Force Run to exit via ctx.Done() before any Schedule call. Wait for
	// it to fully drain (which closes doneCh by reading it directly is
	// awkward; instead, briefly poll on a separate Stop-less path). The
	// simplest deterministic signal: cancel, then sleep just enough for
	// the goroutine to hit the ctx.Done() branch and close doneCh.
	runCancel()
	time.Sleep(50 * time.Millisecond)

	// Now Schedule a late heartbeat. Run is gone; only Stop's defensive
	// flush can persist this.
	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	if got := sched.PendingCount(); got != 1 {
		t.Fatalf("expected pending=1 before Stop, got %d", got)
	}

	sched.Stop()

	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("expected pending=0 after Stop's defensive flush, got %d", got)
	}
	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(stale.Add(time.Hour)) {
		t.Fatalf("Stop did not flush late Schedule: stale=%s after=%s", stale, lastSeen)
	}
}

// TestBatchedHeartbeatScheduler_FlushIgnoresEmpty exercises the empty-pending
// fast path: a tick with nothing queued must not issue a DB call.
func TestBatchedHeartbeatScheduler_FlushIgnoresEmpty(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0, testHandler.Bus)
	// Just calling FlushNow with nothing queued should not panic or error.
	sched.FlushNow(context.Background())
	if got := sched.PendingCount(); got != 0 {
		t.Fatalf("pending should remain 0, got %d", got)
	}
}

// TestBatchedHeartbeatScheduler_RaceToOfflineSelfHeals confirms the
// next-beat-recovery contract: if the sweeper flips a row to offline between
// Schedule and FlushNow, the bulk UPDATE leaves it offline (no rows
// affected), and the runtime's *next* beat takes the sync path through
// recordHeartbeat → MarkAgentRuntimeOnline to recover.
func TestBatchedHeartbeatScheduler_RaceToOfflineSelfHeals(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	rt := loadRuntime(t, runtimeID)

	sched := NewBatchedHeartbeatScheduler(testHandler.Queries, 0, testHandler.Bus)
	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	// Sweeper races us to offline before the flush.
	setRuntimeStatus(t, runtimeID, "offline")

	sched.FlushNow(context.Background())

	// Bulk UPDATE's status='online' predicate means the row stays offline.
	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "offline" {
		t.Fatalf("expected status=offline after raced flush, got %q", status)
	}

	// Reload and re-Schedule: rt.Status is now offline, so the scheduler
	// takes the sync MarkAgentRuntimeOnline path and the row recovers.
	rt2 := loadRuntime(t, runtimeID)
	if err := sched.Schedule(context.Background(), rt2); err != nil {
		t.Fatalf("recovery Schedule: %v", err)
	}
	status2, _, _ := readRuntimeRow(t, runtimeID)
	if status2 != "online" {
		t.Fatalf("expected sync recovery to flip back to online, got %q", status2)
	}
}

// TestPassthroughHeartbeatScheduler_TouchAndRaceRecovery confirms the legacy
// behavior is preserved end-to-end: an online row gets bumped via Touch, and
// a row whose status was raced to offline between SELECT and Schedule is
// recovered via MarkAgentRuntimeOnline.
func TestPassthroughHeartbeatScheduler_TouchAndRaceRecovery(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	stale := time.Now().Add(-time.Hour)
	setRuntimeLastSeenAt(t, runtimeID, stale)
	rt := loadRuntime(t, runtimeID)

	sched := NewPassthroughHeartbeatScheduler(testHandler.Queries, testHandler.Bus)

	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}
	_, lastSeen, _ := readRuntimeRow(t, runtimeID)
	if !lastSeen.After(stale.Add(time.Minute)) {
		t.Fatalf("passthrough did not bump last_seen_at: stale=%s after=%s", stale, lastSeen)
	}

	// Race: snapshot still says online but DB is now offline.
	rt2 := loadRuntime(t, runtimeID)
	setRuntimeStatus(t, runtimeID, "offline")
	if err := sched.Schedule(context.Background(), rt2); err != nil {
		t.Fatalf("Schedule under race: %v", err)
	}
	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "online" {
		t.Fatalf("expected race recovery via MarkAgentRuntimeOnline, got %q", status)
	}
}

// TestPassthroughHeartbeatScheduler_PublishesOnOfflineToOnline confirms
// NEX-1283's fix: a heartbeat that flips an offline row back online must
// publish a daemon:register("online") event so open workspace screens
// re-fetch runtime state instead of being stuck showing the stale offline
// banner until some unrelated daemon:* event happens to fire.
func TestPassthroughHeartbeatScheduler_PublishesOnOfflineToOnline(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	setRuntimeStatus(t, runtimeID, "offline")
	rt := loadRuntime(t, runtimeID)
	if rt.Status != "offline" {
		t.Fatalf("setup: status=%q want offline", rt.Status)
	}

	var publishes int32
	var lastWorkspaceID, lastAction string
	testHandler.Bus.Subscribe(protocol.EventDaemonRegister, func(e events.Event) {
		atomic.AddInt32(&publishes, 1)
		lastWorkspaceID = e.WorkspaceID
		if payload, ok := e.Payload.(map[string]any); ok {
			lastAction, _ = payload["action"].(string)
		}
	})

	sched := NewPassthroughHeartbeatScheduler(testHandler.Queries, testHandler.Bus)
	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule: %v", err)
	}

	if got := atomic.LoadInt32(&publishes); got != 1 {
		t.Fatalf("expected 1 publish on offline->online, got %d", got)
	}
	if lastWorkspaceID != testWorkspaceID {
		t.Fatalf("expected workspace_id=%q, got %q", testWorkspaceID, lastWorkspaceID)
	}
	if lastAction != "online" {
		t.Fatalf("expected action=%q, got %q", "online", lastAction)
	}
	status, _, _ := readRuntimeRow(t, runtimeID)
	if status != "online" {
		t.Fatalf("expected status=online after flip, got %q", status)
	}
}

// TestPassthroughHeartbeatScheduler_NoPublishOnOnlineToOnline confirms the
// anti-spam half of the fix: a heartbeat that finds the row already online
// must not publish anything, on both the hot Touch path and the
// never-seen/MarkAgentRuntimeOnline path when the row happened to already be
// online (e.g. first heartbeat right after registration).
func TestPassthroughHeartbeatScheduler_NoPublishOnOnlineToOnline(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)
	rt := loadRuntime(t, runtimeID)
	if rt.Status != "online" {
		t.Fatalf("setup: status=%q want online", rt.Status)
	}

	var publishes int32
	testHandler.Bus.Subscribe(protocol.EventDaemonRegister, func(e events.Event) {
		atomic.AddInt32(&publishes, 1)
	})

	sched := NewPassthroughHeartbeatScheduler(testHandler.Queries, testHandler.Bus)

	// Hot path: already online with a valid last_seen_at -> Touch, no publish.
	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule (touch path): %v", err)
	}

	// Never-seen path with status already online (e.g. row created online,
	// last_seen_at not yet set) must also not publish: MarkAgentRuntimeOnline
	// runs, but previous_status was already "online", so it's a no-op flip.
	rt.LastSeenAt = pgtype.Timestamptz{}
	if err := sched.Schedule(context.Background(), rt); err != nil {
		t.Fatalf("Schedule (never-seen, already online): %v", err)
	}

	if got := atomic.LoadInt32(&publishes); got != 0 {
		t.Fatalf("expected 0 publishes on online->online, got %d", got)
	}
}

// silenceUnusedPgUUID ensures the package compiles even if no other test
// happens to reference pgtype after future edits trim imports.
var _ = pgtype.UUID{}
