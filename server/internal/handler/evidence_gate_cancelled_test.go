package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// helpers to build run-history fixtures for blockingTaskRunReason.

func runTaskFixture(idByte byte, status string, createdAt time.Time, failureReason string) db.AgentTaskQueue {
	task := db.AgentTaskQueue{
		ID:        pgtype.UUID{Bytes: [16]byte{idByte}, Valid: true},
		Status:    status,
		CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
	}
	if failureReason != "" {
		task.FailureReason = pgtype.Text{String: failureReason, Valid: true}
	}
	return task
}

// tasks are returned by ListTasksByIssue ordered created_at DESC; mirror that here.
func runHistoryDesc(tasks ...db.AgentTaskQueue) []db.AgentTaskQueue {
	return tasks
}

func TestBlockingTaskRunReasonFailedRunAlwaysBlocks(t *testing.T) {
	base := time.Date(2026, 6, 26, 4, 0, 0, 0, time.UTC)
	tasks := runHistoryDesc(
		runTaskFixture(2, "failed", base.Add(2*time.Minute), "agent_error"),
		runTaskFixture(1, "completed", base.Add(time.Minute), ""),
	)

	// failed run blocks regardless of the close path.
	for _, allow := range []bool{false, true} {
		reason := blockingTaskRunReason(tasks, allow)
		if reason == "" {
			t.Fatalf("failed run must block (allowBenignCancelled=%v), got empty reason", allow)
		}
		if !strings.Contains(reason, "agent_error") {
			t.Fatalf("failed reason should cite failure_reason, got %q", reason)
		}
	}
}

func TestBlockingTaskRunReasonUnrecoveredCancelledBlocks(t *testing.T) {
	base := time.Date(2026, 6, 26, 4, 0, 0, 0, time.UTC)
	// cancelled is the most recent run; the only completed run predates it.
	tasks := runHistoryDesc(
		runTaskFixture(2, "cancelled", base.Add(2*time.Minute), ""),
		runTaskFixture(1, "completed", base.Add(time.Minute), ""),
	)

	reason := blockingTaskRunReason(tasks, false)
	if reason == "" {
		t.Fatalf("unrecovered cancelled run (no completed run after it) must block")
	}
	if !strings.Contains(reason, "cancelled") {
		t.Fatalf("reason should cite cancelled run, got %q", reason)
	}
}

func TestBlockingTaskRunReasonRecoveredCancelledPasses(t *testing.T) {
	base := time.Date(2026, 6, 26, 4, 0, 0, 0, time.UTC)
	// benign pattern: cancel -> re-dispatch -> complete. The completed run is newer.
	tasks := runHistoryDesc(
		runTaskFixture(3, "completed", base.Add(3*time.Minute), ""),
		runTaskFixture(2, "cancelled", base.Add(2*time.Minute), ""),
		runTaskFixture(1, "completed", base.Add(time.Minute), ""),
	)

	if reason := blockingTaskRunReason(tasks, false); reason != "" {
		t.Fatalf("recovered cancelled run (completed run created after it) must pass, got %q", reason)
	}
}

func TestBlockingTaskRunReasonClosePathIgnoresAccumulatedCancelled(t *testing.T) {
	base := time.Date(2026, 6, 26, 4, 0, 0, 0, time.UTC)
	// verification-complete close path: even an unrecovered (latest) cancelled run
	// is ignored when allowBenignCancelled is true and there is no failed run.
	tasks := runHistoryDesc(
		runTaskFixture(3, "cancelled", base.Add(3*time.Minute), ""),
		runTaskFixture(2, "cancelled", base.Add(2*time.Minute), ""),
		runTaskFixture(1, "completed", base.Add(time.Minute), ""),
	)

	if reason := blockingTaskRunReason(tasks, true); reason != "" {
		t.Fatalf("close path must ignore accumulated benign cancelled runs, got %q", reason)
	}
	// without the close path the same history must still block (latest cancelled is unrecovered).
	if reason := blockingTaskRunReason(tasks, false); reason == "" {
		t.Fatalf("without close path, unrecovered cancelled must still block")
	}
}

func TestBlockingTaskRunReasonClosePathStillBlocksFailedRun(t *testing.T) {
	base := time.Date(2026, 6, 26, 4, 0, 0, 0, time.UTC)
	tasks := runHistoryDesc(
		runTaskFixture(2, "failed", base.Add(2*time.Minute), "agent_error"),
		runTaskFixture(1, "cancelled", base.Add(time.Minute), ""),
	)

	if reason := blockingTaskRunReason(tasks, true); reason == "" {
		t.Fatalf("close path must NOT relax a genuine failed run")
	}
}

func TestBlockingTaskRunReasonCompletedOnlyPasses(t *testing.T) {
	base := time.Date(2026, 6, 26, 4, 0, 0, 0, time.UTC)
	tasks := runHistoryDesc(
		runTaskFixture(2, "completed", base.Add(2*time.Minute), ""),
		runTaskFixture(1, "completed", base.Add(time.Minute), ""),
	)

	if reason := blockingTaskRunReason(tasks, false); reason != "" {
		t.Fatalf("completed-only history must pass, got %q", reason)
	}
}
