-- Structured verdicts are additive; old free-form comments never auto-close.
ALTER TABLE comment
    ADD COLUMN verdict TEXT NULL CHECK (verdict IN ('PASS', 'FAIL')),
    ADD COLUMN verified_ref TEXT NULL,
    ADD COLUMN criteria_version TEXT NULL,
    ADD COLUMN verifier_agent_id UUID NULL REFERENCES agent(id) ON DELETE SET NULL;

ALTER TABLE issue
    ADD COLUMN auto_close_allowed BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN implementation_agent_id UUID NULL REFERENCES agent(id) ON DELETE SET NULL,
    ADD COLUMN current_ref TEXT NULL,
    ADD COLUMN external_validation_required BOOLEAN NOT NULL DEFAULT FALSE;
