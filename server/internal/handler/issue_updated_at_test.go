package handler

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueResponseUpdatedAtPreservesSameSecondVersions(t *testing.T) {
	before := time.Date(2026, time.November, 24, 0, 0, 0, 123456000, time.UTC)
	after := before.Add(500*time.Millisecond + time.Microsecond)
	timestamp := func(value time.Time) pgtype.Timestamptz {
		return pgtype.Timestamptz{Time: value, Valid: true}
	}
	serializers := map[string]func(pgtype.Timestamptz) IssueResponse{
		"issue": func(updatedAt pgtype.Timestamptz) IssueResponse {
			return issueToResponse(db.Issue{CreatedAt: timestamp(before), UpdatedAt: updatedAt}, "ISO")
		},
		"list": func(updatedAt pgtype.Timestamptz) IssueResponse {
			return issueListRowToResponse(db.ListIssuesRow{CreatedAt: timestamp(before), UpdatedAt: updatedAt}, "ISO")
		},
		"open": func(updatedAt pgtype.Timestamptz) IssueResponse {
			return openIssueRowToResponse(db.ListOpenIssuesRow{CreatedAt: timestamp(before), UpdatedAt: updatedAt}, "ISO")
		},
	}
	for name, serialize := range serializers {
		t.Run(name, func(t *testing.T) {
			for _, value := range []time.Time{before, after, before.Truncate(time.Second)} {
				response := serialize(timestamp(value))
				parsed, err := time.Parse(time.RFC3339, response.UpdatedAt)
				if err != nil || !parsed.Equal(value) {
					t.Fatalf("updated_at = %q, want %s (parse error: %v)", response.UpdatedAt, value.Format(time.RFC3339Nano), err)
				}
				if response.CreatedAt != before.Format(time.RFC3339) {
					t.Fatalf("created_at = %q, want %s", response.CreatedAt, before.Format(time.RFC3339))
				}
			}
		})
	}
}
