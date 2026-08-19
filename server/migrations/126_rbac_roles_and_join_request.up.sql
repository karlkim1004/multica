ALTER TABLE member DROP CONSTRAINT member_role_check;
ALTER TABLE member ADD CONSTRAINT member_role_check
    CHECK (role IN ('owner', 'admin', 'member', 'super_user', 'general_user'));

ALTER TABLE workspace_invitation DROP CONSTRAINT workspace_invitation_role_check;
ALTER TABLE workspace_invitation ADD CONSTRAINT workspace_invitation_role_check
    CHECK (role IN ('admin', 'member', 'super_user', 'general_user'));

CREATE TABLE workspace_join_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at TIMESTAMPTZ,
    reviewed_by UUID REFERENCES "user"(id),
    rejection_reason TEXT
);

CREATE UNIQUE INDEX idx_workspace_join_request_unique_pending
    ON workspace_join_request(workspace_id, user_id) WHERE status = 'pending';
CREATE INDEX idx_workspace_join_request_pending_workspace
    ON workspace_join_request(workspace_id, requested_at) WHERE status = 'pending';
