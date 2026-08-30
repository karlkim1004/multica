ALTER TABLE issue DROP COLUMN IF EXISTS external_validation_required, DROP COLUMN IF EXISTS current_ref, DROP COLUMN IF EXISTS implementation_agent_id, DROP COLUMN IF EXISTS auto_close_allowed;
ALTER TABLE comment DROP COLUMN IF EXISTS verifier_agent_id, DROP COLUMN IF EXISTS criteria_version, DROP COLUMN IF EXISTS verified_ref, DROP COLUMN IF EXISTS verdict;
