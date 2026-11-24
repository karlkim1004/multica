package handler

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueResponseUpdatedAtPreservesSameSecondVersions(t *testing.T) {
	before := time.Date(2026, time.November, 24, 0, 0, 0, 123450000, time.UTC)
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
			for _, version := range []struct {
				value time.Time
				want  string
			}{
				{before, "2026-11-24T00:00:00.123450Z"},
				{after, "2026-11-24T00:00:00.623451Z"},
				{before.Truncate(time.Second), "2026-11-24T00:00:00.000000Z"},
				{before.In(time.FixedZone("KST", 9*60*60)), "2026-11-24T09:00:00.123450+09:00"},
			} {
				response := serialize(timestamp(version.value))
				if response.UpdatedAt != version.want {
					t.Errorf("updated_at = %q, want %q", response.UpdatedAt, version.want)
				}
				parsed, err := time.Parse(time.RFC3339, response.UpdatedAt)
				if err != nil || !parsed.Equal(version.value) {
					t.Fatalf("updated_at = %q, want %s (parse error: %v)", response.UpdatedAt, version.value, err)
				}
				if response.CreatedAt != before.Format(time.RFC3339) {
					t.Fatalf("created_at = %q, want %s", response.CreatedAt, before.Format(time.RFC3339))
				}
			}
		})
	}
}
