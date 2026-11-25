package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Run the same local policy as the tool hooks after resolving the comment body,
// before any API request or attachment upload.
func checkCommentMemoryAction(ctx context.Context, content string) error {
	script := strings.TrimSpace(os.Getenv("MULTICA_MEMORY_ACTION_GUARD"))
	if script == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		script = filepath.Join(home, "nexai", "scripts", "governance_gate_hook.py")
		if _, err := os.Stat(script); errors.Is(err, os.ErrNotExist) {
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "python3", script, "--memory-action-check")
	command.Stdin = strings.NewReader(content)
	output, err := command.CombinedOutput()
	if err == nil {
		if strings.TrimSpace(string(output)) != "allow" {
			return errors.New("comment memory action guard returned an invalid response")
		}
		return nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && strings.HasPrefix(string(output), "차단 게이트-공유기억:") {
		return errors.New(strings.TrimSpace(string(output)))
	}
	return fmt.Errorf("comment memory action guard unavailable: %w", err)
}
