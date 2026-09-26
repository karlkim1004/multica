# Autopilots source map

- `server/cmd/multica/cmd_autopilot.go` registers `list`, `get`, `create`, `update`, `delete`, `trigger`, `runs`, `trigger-add`, `trigger-update`, `trigger-delete`, and `trigger-rotate-url`.
- The CLI maps reads/writes to `/api/autopilots`, `/api/autopilots/{id}`, `/api/autopilots/{id}/trigger`, `/api/autopilots/{id}/runs`, and trigger subroutes.
- `server/internal/service/autopilot.go` has `DispatchAutopilot`, creates `autopilot_run`, and switches on `execution_mode`.
- `create_issue` calls `dispatchCreateIssue`; `run_only` calls `dispatchRunOnly`.
- `resolveAutopilotLeader` resolves squad-assigned autopilots to the squad leader.
- `AgentReadiness` blocks archived/runtime-unready agents before enqueue.
- `server/cmd/server/router.go` exposes authenticated `/api/autopilots` routes and unauthenticated webhook ingress `/api/webhooks/autopilots/{token}`.

## Chat delivery
- `server/migrations/131_autopilot_chat_delivery.up.sql`: opt-in destination and unique run message key.
- `server/pkg/db/queries/autopilot.sql`: creator/member/private-agent authorization and idempotent insertion.
- `server/internal/service/autopilot.go`: completion delivery and empty/failure behavior.
- `server/cmd/multica/cmd_chat.go`: read-only session discovery.
- `server/cmd/multica/cmd_autopilot.go`: explicit destination flag.
