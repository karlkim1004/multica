-- name: AcquireWorkerPoolLock :one
-- INSERT-or-take-expired is one PostgreSQL statement. Concurrent contenders
-- serialize on the primary key; only the winner receives a returned row.
INSERT INTO worker_pool_lock (
    workspace_id, resource_type, resource_key, holder_id, expires_at
) VALUES (
    $1, $2, $3, $4, now() + make_interval(secs => @lease_seconds::double precision)
)
ON CONFLICT (workspace_id, resource_type, resource_key) DO UPDATE
SET holder_id = EXCLUDED.holder_id,
    expires_at = EXCLUDED.expires_at,
    updated_at = now()
WHERE worker_pool_lock.expires_at <= now()
RETURNING *;

-- name: ReleaseWorkerPoolLock :execrows
DELETE FROM worker_pool_lock
WHERE workspace_id = $1
  AND resource_type = $2
  AND resource_key = $3
  AND holder_id = $4;
