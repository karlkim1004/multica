package service

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

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
