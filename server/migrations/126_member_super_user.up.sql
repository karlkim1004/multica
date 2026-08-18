-- Additive role expansion. Existing owner/admin/member rows remain valid.
ALTER TABLE member DROP CONSTRAINT IF EXISTS member_role_check;
ALTER TABLE member
    ADD CONSTRAINT member_role_check
    CHECK (role IN ('owner', 'admin', 'super_user', 'member'));
