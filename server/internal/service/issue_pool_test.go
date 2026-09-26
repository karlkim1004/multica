package service

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

func TestPoolOutcomeBlocksRecovery(t *testing.T) {
	agent := db.Agent{ID: pgtype.UUID{Bytes: [16]byte{1}, Valid: true}, RuntimeID: pgtype.UUID{Bytes: [16]byte{2}, Valid: true}}
	auth := db.AgentTaskQueue{AgentID: agent.ID, RuntimeID: agent.RuntimeID, Status: "failed",
		FailureReason: pgtype.Text{String: taskfailure.ReasonAgentProviderAuthOrAccess.String(), Valid: true}}
	network := auth
	network.FailureReason.String = taskfailure.ReasonAgentProviderNetwork.String()
	if !poolFailuresBlockRecovery([]db.AgentTaskQueue{network, auth}, agent) {
		t.Fatal("a newer network error must not hide unresolved authentication failure")
	}
	if poolFailuresBlockRecovery(nil, agent) {
		t.Fatal("no failures since the last success should allow recovery")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*db.AgentTaskQueue)
		want   bool
	}{
		{"auth failure", func(*db.AgentTaskQueue) {}, true},
		{"legacy unknown OAuth", func(q *db.AgentTaskQueue) {
			q.FailureReason.String = taskfailure.ReasonAgentUnknown.String()
			q.Error = pgtype.Text{String: "OAuth token has expired. Please obtain a new token or refresh your existing token.", Valid: true}
		}, true},
		{"successful authentication probe", func(q *db.AgentTaskQueue) { q.Status = "completed" }, false},
		{"different provider runtime", func(q *db.AgentTaskQueue) { q.RuntimeID.Bytes[0] = 3 }, false},
		{"different agent", func(q *db.AgentTaskQueue) { q.AgentID.Bytes[0] = 3 }, false},
		{"transient network failure", func(q *db.AgentTaskQueue) { q.FailureReason.String = taskfailure.ReasonAgentProviderNetwork.String() }, false},
		{"unrelated unknown error", func(q *db.AgentTaskQueue) { q.FailureReason.String = taskfailure.ReasonAgentUnknown.String() }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := auth
			tc.mutate(&q)
			if got := poolOutcomeBlocksRecovery(q, agent); got != tc.want {
				t.Fatalf("blocked = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPoolIssueMayDispatch(t *testing.T) {
	tests := []struct {
		name     string
		metadata string
		want     bool
	}{
		{"no owner", `{}`, true},
		{"agent owner", `{"waiting_on":"agent:worker"}`, true},
		{"ceo owner", `{"waiting_on":"ceo"}`, false},
		{"external owner", `{"waiting_on":"external"}`, false},
		{"invalid metadata", `{`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := poolIssueMayDispatch([]byte(tt.metadata)); got != tt.want {
				t.Fatalf("poolIssueMayDispatch(%s) = %v, want %v", tt.metadata, got, tt.want)
			}
		})
	}
}

func TestClaimPoolSweepSlotIsScopedToWorkspace(t *testing.T) {
	service := &IssueService{}
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	first := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	second := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}

	if !service.claimPoolSweepSlot(first, now) {
		t.Fatal("first workspace should acquire its initial scan slot")
	}
	if !service.claimPoolSweepSlot(second, now) {
		t.Fatal("second workspace should not be suppressed by the first workspace cooldown")
	}
	if service.claimPoolSweepSlot(first, now.Add(time.Second)) {
		t.Fatal("same workspace should respect the cooldown")
	}
}
