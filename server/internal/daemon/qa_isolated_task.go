package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func (d *Daemon) runIsolatedQATask(ctx context.Context, task Task, taskLog *slog.Logger, capture func(context.Context, string) error) (TaskResult, error) {
	backend, err := agent.New("codex", agent.Config{
		ExecutablePath: "/opt/nex1979-qa/launch-codex",
		Logger:         taskLog,
	})
	if err != nil {
		return TaskResult{}, err
	}
	if err := d.client.StartTask(ctx, task.ID); err != nil {
		return TaskResult{}, fmt.Errorf("start synthetic QA task: %w", err)
	}
	stopHeartbeat := d.startTaskRunningHeartbeatExtender(ctx, task, taskLog)
	defer stopHeartbeat()

	result, tools, err := d.executeAndDrain(ctx, backend, BuildPrompt(task, "codex"), agent.ExecOptions{
		BeforeCodexTurn:       capture,
		CodexIsolatedLauncher: true,
		ExecutionPolicy:       task.ExecutionPolicy,
		Cwd:                   "/work",
		Model:                 "gpt-6-luna",
		Timeout:               90 * time.Second,
	}, taskLog, task.ID)
	if err != nil {
		return TaskResult{}, err
	}
	taskLog.Info("synthetic QA child finished", "status", result.Status, "session_id", result.SessionID, "tools", tools)
	// /work exists only in the child's private mount namespace.
	return d.taskResultFromAgent(result, "codex", &execenv.Environment{}, taskLog)
}
