-- name: CreateWorkspaceJoinRequest :one
INSERT INTO workspace_join_request (workspace_id, user_id)
VALUES ($1, $2)
RETURNING *;

-- name: GetWorkspaceJoinRequest :one
SELECT * FROM workspace_join_request
WHERE id = $1;

-- name: GetPendingWorkspaceJoinRequest :one
SELECT * FROM workspace_join_request
WHERE workspace_id = $1 AND user_id = $2 AND status = 'pending';

-- name: ListPendingWorkspaceJoinRequests :many
SELECT * FROM workspace_join_request
WHERE workspace_id = $1 AND status = 'pending'
ORDER BY requested_at ASC;

-- name: ApproveWorkspaceJoinRequest :one
UPDATE workspace_join_request
SET status = 'approved', reviewed_at = now(), reviewed_by = $2, rejection_reason = NULL
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: RejectWorkspaceJoinRequest :one
UPDATE workspace_join_request
SET status = 'rejected', reviewed_at = now(), reviewed_by = $2, rejection_reason = $3
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: CancelWorkspaceJoinRequest :one
UPDATE workspace_join_request
SET status = 'cancelled'
WHERE id = $1 AND user_id = $2 AND status = 'pending'
RETURNING *;
