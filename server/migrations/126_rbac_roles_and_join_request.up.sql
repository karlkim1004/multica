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

-- Down snapshots records that the pre-126 schema cannot express. Create
-- empty copies here so a normal first up has no special case.
CREATE TABLE IF NOT EXISTS member_role_rollback_126 (
    id UUID PRIMARY KEY, workspace_id UUID NOT NULL, user_id UUID NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('super_user', 'general_user')),
    created_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS workspace_invitation_role_rollback_126 (
    id UUID PRIMARY KEY, workspace_id UUID NOT NULL, inviter_id UUID NOT NULL,
    invitee_email TEXT NOT NULL, invitee_user_id UUID,
    role TEXT NOT NULL CHECK (role IN ('super_user', 'general_user')),
    status TEXT NOT NULL, created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL, expires_at TIMESTAMPTZ NOT NULL
);
CREATE TABLE IF NOT EXISTS workspace_join_request_rollback_126 (
    id UUID PRIMARY KEY, workspace_id UUID NOT NULL, user_id UUID NOT NULL,
    status TEXT NOT NULL, requested_at TIMESTAMPTZ NOT NULL,
    reviewed_at TIMESTAMPTZ, reviewed_by UUID, rejection_reason TEXT
);
CREATE TABLE IF NOT EXISTS lark_user_binding_member_rollback_126 (
    id UUID PRIMARY KEY, workspace_id UUID NOT NULL, multica_user_id UUID NOT NULL,
    installation_id UUID NOT NULL, lark_open_id TEXT NOT NULL, union_id TEXT,
    bound_at TIMESTAMPTZ NOT NULL
);

INSERT INTO member (id, workspace_id, user_id, role, created_at)
SELECT id, workspace_id, user_id, role, created_at FROM member_role_rollback_126
ON CONFLICT (workspace_id, user_id) DO UPDATE SET role = EXCLUDED.role;

INSERT INTO workspace_invitation (id, workspace_id, inviter_id, invitee_email, invitee_user_id, role, status, created_at, updated_at, expires_at)
SELECT id, workspace_id, inviter_id, invitee_email, invitee_user_id, role, status, created_at, updated_at, expires_at FROM workspace_invitation_role_rollback_126
ON CONFLICT (id) DO UPDATE SET workspace_id = EXCLUDED.workspace_id, inviter_id = EXCLUDED.inviter_id, invitee_email = EXCLUDED.invitee_email, invitee_user_id = EXCLUDED.invitee_user_id, role = EXCLUDED.role, status = EXCLUDED.status, created_at = EXCLUDED.created_at, updated_at = EXCLUDED.updated_at, expires_at = EXCLUDED.expires_at;

INSERT INTO workspace_join_request (id, workspace_id, user_id, status, requested_at, reviewed_at, reviewed_by, rejection_reason)
SELECT id, workspace_id, user_id, status, requested_at, reviewed_at, reviewed_by, rejection_reason FROM workspace_join_request_rollback_126
ON CONFLICT (id) DO UPDATE SET workspace_id = EXCLUDED.workspace_id, user_id = EXCLUDED.user_id, status = EXCLUDED.status, requested_at = EXCLUDED.requested_at, reviewed_at = EXCLUDED.reviewed_at, reviewed_by = EXCLUDED.reviewed_by, rejection_reason = EXCLUDED.rejection_reason;

INSERT INTO lark_user_binding (id, workspace_id, multica_user_id, installation_id, lark_open_id, union_id, bound_at)
SELECT id, workspace_id, multica_user_id, installation_id, lark_open_id, union_id, bound_at FROM lark_user_binding_member_rollback_126
ON CONFLICT (id) DO UPDATE SET workspace_id = EXCLUDED.workspace_id, multica_user_id = EXCLUDED.multica_user_id, installation_id = EXCLUDED.installation_id, lark_open_id = EXCLUDED.lark_open_id, union_id = EXCLUDED.union_id, bound_at = EXCLUDED.bound_at;

DROP TABLE IF EXISTS lark_user_binding_member_rollback_126;
DROP TABLE IF EXISTS workspace_join_request_rollback_126;
DROP TABLE IF EXISTS workspace_invitation_role_rollback_126;
DROP TABLE IF EXISTS member_role_rollback_126;
