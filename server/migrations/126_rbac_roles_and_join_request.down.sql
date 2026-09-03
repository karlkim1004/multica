DROP TABLE IF EXISTS workspace_join_request;

ALTER TABLE workspace_invitation DROP CONSTRAINT workspace_invitation_role_check;
ALTER TABLE workspace_invitation ADD CONSTRAINT workspace_invitation_role_check
    CHECK (role IN ('admin', 'member'));

ALTER TABLE member DROP CONSTRAINT member_role_check;
ALTER TABLE member ADD CONSTRAINT member_role_check
    CHECK (role IN ('owner', 'admin', 'member'));
