package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// HasPendingMembershipApplication is intentionally a narrow boolean query so
// workspace authorization can distinguish a pending applicant from an unknown
// non-member without exposing application data.
func (q *Queries) HasPendingMembershipApplication(ctx context.Context, workspaceID, userID pgtype.UUID) (bool, error) {
	var pending bool
	err := q.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM workspace_membership_application
			WHERE workspace_id = $1 AND user_id = $2 AND status = 'pending'
		)
	`, workspaceID, userID).Scan(&pending)
	return pending, err
}
