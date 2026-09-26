---
name: multica-autopilots
description: "Use when creating, updating, inspecting, triggering, or debugging Multica autopilots. Covers the full chain: schedule/webhook/manual trigger, create_issue vs run_only execution, agent/squad leader admission, runs, created issues/tasks, webhook URL rotation, and side-effect boundaries."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Autopilots

## Quick start

Autopilots are durable automations. Read before mutating:

```bash
multica autopilot list --output json
multica autopilot get <autopilot-id> --output json
multica autopilot runs <autopilot-id> --output json
```

Do not run `trigger`, `delete`, `trigger-delete`, or `trigger-rotate-url` to test. Those are real side effects.

## Core model

An autopilot is not an agent. It is a rule that dispatches work to an agent, or to a squad's leader agent.

The chain is: trigger fires (`schedule`, `webhook`, or `manual`) -> `autopilot_run` row -> `execution_mode` decides output -> assignee readiness check -> issue/task execution -> run status sync.

Execution modes:

- `create_issue` creates a Multica issue, making the run visible as issue state.
- `run_only` creates an agent task directly. No issue is created; any durable
  report location has to come from other task context or instructions.

`issue-title-template` only supports `{{date}}`. Do not invent `{{trigger_id}}`, `{{branch}}`, or other variables.

## CLI

```bash
multica autopilot list --output json
multica autopilot get <autopilot-id> --output json
multica autopilot create --title "<title>" --description "<task prompt>" --agent <agent-name-or-id> --mode create_issue|run_only --output json
multica autopilot update <autopilot-id> --status active|paused --output json
multica autopilot runs <autopilot-id> --output json
multica autopilot trigger-add <autopilot-id> --kind schedule --cron "0 9 * * *" --timezone Asia/Shanghai --output json
multica autopilot trigger-add <autopilot-id> --kind webhook --label "ci" --output json
multica autopilot trigger <autopilot-id> --output json
multica autopilot trigger-rotate-url <autopilot-id> <trigger-id> --yes --output json
```

Use `trigger` only when the user explicitly asks for a manual run. Use `trigger-rotate-url` only when rotating a webhook URL; the old URL stops being valid.

Webhook trigger output can include a URL/token. Do not paste webhook tokens or signing material into comments, logs, docs, or PRs. Redact secrets.

## Debugging

For "why didn't it run":

1. `multica autopilot get <id> --output json` — status, mode, assignee, triggers.
2. `multica autopilot runs <id> --output json` — run status and failure reason.
3. If assigned to a squad, inspect the squad: `multica squad get <squad-id> --output json`; execution goes to the leader.
4. Inspect the target agent/runtime: `multica agent get <agent-id> --output json` and `multica runtime list --output json`.
5. For `create_issue`, inspect the created issue if the run records one.

## Side effects

These mutate durable state or start work: `create`, `update`, `delete`, trigger add/update/delete/rotate, `trigger`, and webhook calls to `/api/webhooks/autopilots/{token}`.

More source-backed details: `references/autopilots-source-map.md`.

## Opt-in chat delivery

`multica chat list --output json` and `multica chat get <id> --output json` read the authenticated user's sessions. `multica chat messages <id> --output json` reads persisted messages to verify delivery. Select an explicit session; never guess the newest session.
`multica autopilot create|update --delivery-chat-session <id>` enables delivery; update with an empty value clears it. API field: `delivery_chat_session_id` (nullable UUID).
Only `run_only` agent autopilots support this. The target must be an active session owned by the autopilot creator in the same workspace with the same agent. Only the creator may edit an autopilot with delivery enabled. Workspace membership and private-agent access are checked when configuring and again atomically when inserting the result.
Delivery uses the current configuration at completion time; changing or clearing the destination affects in-flight runs. Successful nonempty output becomes an assistant message. Empty successful output is silent. Failed/cancelled runs emit a generic failure notice without raw diagnostic secrets. Admission-skipped runs do not deliver.
A persistent unique run key prevents duplicate messages from repeated completion callbacks. No user message or new task is created; `chat:message` refreshes clients without marking the user's active task complete. Run history exposes `delivery_status`: not_requested, pending, delivered, suppressed (empty output), blocked (authorization/configuration), or failed. Database delivery errors are logged and marked failed where the DB is available; a crash may leave pending. `multica autopilot retry-delivery <autopilot-id> <run-id>` retries only the stored result, rechecks authorization, and never runs the agent again. Only the autopilot creator can invoke this. There is no automatic retry worker yet; inspect pending/failed runs and use this explicit recovery command.
