ALTER TABLE agent_task_queue
  DROP COLUMN IF EXISTS running_lease_expires_at;
