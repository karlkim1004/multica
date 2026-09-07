-- Leases, rather than permanent mutexes: a crashed worker loses ownership
-- automatically after its lease expires. resource_key is an issue UUID or a
-- cleaned project-relative file path; its uniqueness makes acquire atomic.
CREATE TABLE worker_pool_lock (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    resource_type TEXT NOT NULL CHECK (resource_type IN ('issue', 'file')),
    resource_key TEXT NOT NULL,
    holder_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, resource_type, resource_key)
);

CREATE INDEX idx_worker_pool_lock_expiry ON worker_pool_lock (expires_at);
