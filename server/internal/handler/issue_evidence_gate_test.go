package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestBlockingTaskRunReasonRejectsCancelledRunByDefault(t *testing.T) {
	reason := blockingTaskRunReason([]db.AgentTaskQueue{
		taskRunForEvidenceGate("cancelled", "user_cancelled", time.Now()),
		taskRunForEvidenceGate("completed", "", time.Now().Add(time.Minute)),
	})

	if !strings.Contains(reason, "cancelled issue run") {
		t.Fatalf("expected default cancelled run to block, got %q", reason)
	}
}

func TestBlockingTaskRunReasonRejectsSystemCancellationWithoutLaterSuccess(t *testing.T) {
	reason := blockingTaskRunReason([]db.AgentTaskQueue{
		taskRunForEvidenceGate("cancelled", "system_restart", time.Now()),
	})

	if !strings.Contains(reason, "후속 성공 검증 run") {
		t.Fatalf("expected system cancellation without later success to block, got %q", reason)
	}
}

func TestBlockingTaskRunReasonAllowsSystemCancellationWithLaterSuccess(t *testing.T) {
	now := time.Now()
	reason := blockingTaskRunReason([]db.AgentTaskQueue{
		taskRunForEvidenceGate("cancelled", "duplicate_dispatch", now),
		taskRunForEvidenceGate("completed", "", now.Add(time.Minute)),
	})

	if reason != "" {
		t.Fatalf("expected system cancellation with later success to be ignored, got %q", reason)
	}
}

func taskRunForEvidenceGate(status, failureReason string, completedAt time.Time) db.AgentTaskQueue {
	return db.AgentTaskQueue{
		Status: status,
		FailureReason: pgtype.Text{
			String: failureReason,
			Valid:  failureReason != "",
		},
		CompletedAt: pgtype.Timestamptz{
			Time:  completedAt,
			Valid: true,
		},
		CreatedAt: pgtype.Timestamptz{
			Time:  completedAt,
			Valid: true,
		},
	}
}
