DROP INDEX chat_message_autopilot_run_unique;
ALTER TABLE chat_message DROP COLUMN autopilot_run_id;
ALTER TABLE autopilot DROP COLUMN delivery_chat_session_id;
