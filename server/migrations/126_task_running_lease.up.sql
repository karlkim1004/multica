-- NEX-1032: reintroduces a per-task lease for the 'running' phase.
--
-- Migration 069 dropped agent_task_queue.last_heartbeat_at because "runtime
-- liveness is owned by agent_runtime.last_seen_at ... and no consumer was
-- ever built". That held until the incident this migration fixes: a daemon
-- process can stay alive and keep heartbeating (agent_runtime.last_seen_at
-- stays fresh) while a single task's per-task watcher goroutine hangs
-- forever. Runtime-level liveness cannot detect that — only a lease that is
-- renewed by the specific in-flight task can.
--
-- running_lease_expires_at is set on StartAgentTask and renewed by the
-- daemon's heartbeat (ExtendAgentTaskRunningLease) every
-- MULTICA_TASK_RUNNING_HEARTBEAT_INTERVAL (default 30s) while status is
-- 'running'. FailStaleTasks reaps a 'running' task once this expires
-- (default TTL 120s), independent of the coarser started_at-only backstop
-- (runningTimeoutSeconds, 2.5h) that stays in place for tasks predating the
-- lease or whose daemon died mid-heartbeat.
ALTER TABLE agent_task_queue
  ADD COLUMN running_lease_expires_at TIMESTAMPTZ;
