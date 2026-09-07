-- A validator is an explicitly registered workspace capability.  The default
-- is deny so existing agents cannot accidentally acquire close authority.
ALTER TABLE agent
    ADD COLUMN is_validator BOOLEAN NOT NULL DEFAULT FALSE;

-- A verdict must be tied to the same acceptance-criteria revision that the
-- issue opted into.  NULL means this issue is not eligible for auto-close.
ALTER TABLE issue
    ADD COLUMN auto_close_criteria_version TEXT NULL;
