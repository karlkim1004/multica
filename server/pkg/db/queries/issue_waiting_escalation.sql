-- NEX-1043: automatic SLA escalation for issues whose ownership metadata
-- (waiting_on / unblock_condition / waiting_since) says an agent is holding
-- the ball. Two stages, both driven off waiting_since / escalation_recalled_at
-- rather than parsed comment prose:
--   1. recall  — waiting_since is older than the recall threshold and no
--      recall has been issued yet.
--   2. reassign — a recall was issued and the reassign threshold has since
--      elapsed with no observable change on the issue.
-- waiting_on = "ceo" never matches either query (both filter on
-- `LIKE 'agent:%'`), which is what guarantees the CEO queue is never
-- auto-recalled or auto-reassigned (completion condition 5).

-- name: SelectIssuesNeedingWaitingRecall :many
-- The CASE-guarded cast means a malformed waiting_since value degrades to
-- NULL (excluded) instead of erroring the whole sweep tick for one bad row.
SELECT *
FROM issue
WHERE status IN ('blocked', 'in_review')
  AND metadata->>'waiting_on' LIKE 'agent:%'
  AND metadata ? 'waiting_since'
  AND NOT (metadata ? 'escalation_recalled_at')
  AND (CASE WHEN metadata->>'waiting_since' ~ '^\d{4}-\d{2}-\d{2}'
       THEN (metadata->>'waiting_since')::timestamptz END)
      < now() - make_interval(secs => @recall_after_secs::double precision)
ORDER BY (CASE WHEN metadata->>'waiting_since' ~ '^\d{4}-\d{2}-\d{2}'
       THEN (metadata->>'waiting_since')::timestamptz END) ASC;

-- name: SelectIssuesNeedingWaitingReassign :many
-- updated_at <= escalation_recalled_at + tolerance is the "nothing happened
-- since the recall" check: the recall write itself bumps updated_at (via
-- SetIssueMetadataKey) to ~escalation_recalled_at, so any later write by the
-- assignee (status change, fresh waiting_since, any other field) advances
-- updated_at past that point and correctly excludes the row here.
SELECT *
FROM issue
WHERE status IN ('blocked', 'in_review')
  AND metadata->>'waiting_on' LIKE 'agent:%'
  AND metadata ? 'escalation_recalled_at'
  AND (CASE WHEN metadata->>'escalation_recalled_at' ~ '^\d{4}-\d{2}-\d{2}'
       THEN (metadata->>'escalation_recalled_at')::timestamptz END)
      < now() - make_interval(secs => @reassign_after_secs::double precision)
  AND updated_at <= (CASE WHEN metadata->>'escalation_recalled_at' ~ '^\d{4}-\d{2}-\d{2}'
       THEN (metadata->>'escalation_recalled_at')::timestamptz END) + interval '30 seconds'
ORDER BY (CASE WHEN metadata->>'escalation_recalled_at' ~ '^\d{4}-\d{2}-\d{2}'
       THEN (metadata->>'escalation_recalled_at')::timestamptz END) ASC;

-- name: ReassignIssueWaitingToCeo :one
-- escalation_recalled_at is removed (not left stale) so the row stops
-- matching SelectIssuesNeedingWaitingReassign, and waiting_on = "ceo" drops
-- it out of SelectIssuesNeedingWaitingRecall's agent:% predicate — together
-- these guarantee no further automatic recall/reassign fires once a wait is
-- CEO-owned (completion condition 5).
UPDATE issue SET
    metadata = (metadata - 'escalation_recalled_at') || jsonb_build_object(
        'waiting_on', 'ceo',
        'unblock_condition', @unblock_condition::text,
        'waiting_since', @waiting_since::text,
        'escalation_reassigned_from', @reassigned_from::text,
        'escalation_reassigned_at', @reassigned_at::text
    ),
    updated_at = now()
WHERE id = @id AND workspace_id = @workspace_id
RETURNING *;
