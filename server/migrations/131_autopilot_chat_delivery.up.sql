ALTER TABLE autopilot ADD COLUMN delivery_chat_session_id UUID REFERENCES chat_session(id) ON DELETE SET NULL;
ALTER TABLE chat_message ADD COLUMN autopilot_run_id UUID REFERENCES autopilot_run(id) ON DELETE SET NULL;
CREATE UNIQUE INDEX chat_message_autopilot_run_unique ON chat_message(autopilot_run_id) WHERE autopilot_run_id IS NOT NULL;
