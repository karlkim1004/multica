-- The pre-126 schema has no general_user role. Fail closed rather than
-- silently upgrading it to member; sidecars make immediate up→down→up lossless.
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

INSERT INTO member_role_rollback_126 (id, workspace_id, user_id, role, created_at)
SELECT id, workspace_id, user_id, role, created_at FROM member
WHERE role IN ('super_user', 'general_user')
ON CONFLICT (id) DO UPDATE SET workspace_id = EXCLUDED.workspace_id, user_id = EXCLUDED.user_id, role = EXCLUDED.role, created_at = EXCLUDED.created_at;

INSERT INTO workspace_invitation_role_rollback_126 (id, workspace_id, inviter_id, invitee_email, invitee_user_id, role, status, created_at, updated_at, expires_at)
SELECT id, workspace_id, inviter_id, invitee_email, invitee_user_id, role, status, created_at, updated_at, expires_at FROM workspace_invitation
WHERE role IN ('super_user', 'general_user')
ON CONFLICT (id) DO UPDATE SET workspace_id = EXCLUDED.workspace_id, inviter_id = EXCLUDED.inviter_id, invitee_email = EXCLUDED.invitee_email, invitee_user_id = EXCLUDED.invitee_user_id, role = EXCLUDED.role, status = EXCLUDED.status, created_at = EXCLUDED.created_at, updated_at = EXCLUDED.updated_at, expires_at = EXCLUDED.expires_at;

INSERT INTO workspace_join_request_rollback_126 (id, workspace_id, user_id, status, requested_at, reviewed_at, reviewed_by, rejection_reason)
SELECT id, workspace_id, user_id, status, requested_at, reviewed_at, reviewed_by, rejection_reason FROM workspace_join_request
ON CONFLICT (id) DO UPDATE SET workspace_id = EXCLUDED.workspace_id, user_id = EXCLUDED.user_id, status = EXCLUDED.status, requested_at = EXCLUDED.requested_at, reviewed_at = EXCLUDED.reviewed_at, reviewed_by = EXCLUDED.reviewed_by, rejection_reason = EXCLUDED.rejection_reason;

-- Removing a general_user cascades its Lark binding; snapshot it first.
INSERT INTO lark_user_binding_member_rollback_126 (id, workspace_id, multica_user_id, installation_id, lark_open_id, union_id, bound_at)
SELECT b.id, b.workspace_id, b.multica_user_id, b.installation_id, b.lark_open_id, b.union_id, b.bound_at
FROM lark_user_binding b JOIN member m ON m.workspace_id = b.workspace_id AND m.user_id = b.multica_user_id
WHERE m.role = 'general_user'
ON CONFLICT (id) DO UPDATE SET workspace_id = EXCLUDED.workspace_id, multica_user_id = EXCLUDED.multica_user_id, installation_id = EXCLUDED.installation_id, lark_open_id = EXCLUDED.lark_open_id, union_id = EXCLUDED.union_id, bound_at = EXCLUDED.bound_at;

DELETE FROM member WHERE role = 'general_user';
UPDATE member SET role = 'member' WHERE role = 'super_user';
DELETE FROM workspace_invitation WHERE role = 'general_user';
UPDATE workspace_invitation SET role = 'member' WHERE role = 'super_user';
DROP TABLE IF EXISTS workspace_join_request;

ALTER TABLE workspace_invitation DROP CONSTRAINT workspace_invitation_role_check;
ALTER TABLE workspace_invitation ADD CONSTRAINT workspace_invitation_role_check CHECK (role IN ('admin', 'member'));
ALTER TABLE member DROP CONSTRAINT member_role_check;
ALTER TABLE member ADD CONSTRAINT member_role_check CHECK (role IN ('owner', 'admin', 'member'));
