-- name: CreateWorkspaceJoinCode :one
INSERT INTO workspace_join_code (workspace_id, code, created_by)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetActiveWorkspaceJoinCode :one
SELECT * FROM workspace_join_code
WHERE code = $1 AND revoked_at IS NULL;

-- name: RevokeWorkspaceJoinCode :exec
UPDATE workspace_join_code SET revoked_at = now()
WHERE id = $1 AND workspace_id = $2 AND revoked_at IS NULL;
