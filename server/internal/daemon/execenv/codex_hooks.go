package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// syncCodexHooksAndTrust copies the shared hooks.json into the task CODEX_HOME
// and carries forward an existing trust decision by rewriting only its
// path-qualified key. Missing trust stays missing: the daemon never trusts a
// hook that the user has not trusted in the shared Codex home.
func syncCodexHooksAndTrust(sharedHome, codexHome string) error {
	srcHooks := filepath.Join(sharedHome, "hooks.json")
	dstHooks := filepath.Join(codexHome, "hooks.json")
	if err := syncCopiedFile(srcHooks, dstHooks); err != nil {
		return fmt.Errorf("sync hooks.json: %w", err)
	}
	if _, err := os.Stat(srcHooks); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat hooks.json: %w", err)
	}

	configPath := filepath.Join(codexHome, "config.toml")
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read task config.toml: %w", err)
	}
	oldKey := strconv.Quote(srcHooks + ":pre_tool_use:0:0")
	newKey := strconv.Quote(dstHooks + ":pre_tool_use:0:0")
	updated := strings.ReplaceAll(string(data), oldKey, newKey)
	if updated == string(data) {
		return nil
	}
	if err := os.WriteFile(configPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("rewrite task hook trust key: %w", err)
	}
	return nil
}
