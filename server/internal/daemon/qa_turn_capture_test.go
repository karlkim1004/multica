package daemon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime"
	"strings"

	"github.com/multica-ai/multica/server/pkg/agent"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

const qaTaskID = "018de82d-3efb-7d49-a0f2-5eb023ae35b2"

func TestQAFileTurnSink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	sink := QAFileTurnSink(dir)
	key := QATurnKey{qaTaskID, 1}
	ctx := t.Context()
	const raw = " exact text\n한글 "
	var receipts [8]QATurnReceipt
	var wg sync.WaitGroup
	for i := range receipts {
		wg.Go(func() {
			receipt, err := sink.Commit(ctx, key, raw)
			if err != nil {
				t.Error(err)
			}
			receipts[i] = receipt
		})
	}
	wg.Wait()
	first := receipts[0]
	if first.QATurnKey != key || first.SHA256 != sha256.Sum256([]byte(raw)) || first.StoredAtUTC.IsZero() {
		t.Fatalf("unexpected receipt: %+v", first)
	}
	for _, receipt := range receipts[1:] {
		if receipt != first {
			t.Fatalf("receipt changed on repeated commit: %+v", receipt)
		}
	}
	if _, err := sink.Commit(ctx, key, "different"); err == nil {
		t.Fatal("accepted a conflicting hash")
	}
	next := key
	next.Attempt++
	if _, err := sink.Commit(ctx, next, "different"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("expected exactly two committed attempts: %v, %v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, qaTaskID+"-1.json"))
	var stored qaTurnRecord
	if err != nil || json.Unmarshal(data, &stored) != nil || stored.Text != raw || stored.QATurnReceipt != first {
		t.Fatalf("stored record differs from capture receipt: %v", err)
	}
	info, err := entries[0].Info()
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("expected private capture file: %v %v", info, err)
	}
	done := QACompletedTask{key, "completed"}
	official, err := json.Marshal(done)
	if err != nil || VerifyQACompletion(bytes.NewReader(data), bytes.NewReader(official), bytes.NewReader(official)) != nil {
		t.Fatal("could not verify official task JSON against the durable capture")
	}
	if !matchQACompletion(first, done, done) {
		t.Fatal("rejected matching official completed records")
	}
	for _, wrong := range []QACompletedTask{
		{next, "completed"}, {QATurnKey{"other", 1}, "completed"}, {key, "queued"}, {QATurnKey{qaTaskID, 0}, "completed"},
	} {
		if matchQACompletion(first, wrong, done) || matchQACompletion(first, done, wrong) {
			t.Fatalf("joined invalid official completion: %+v", wrong)
		}
	}
}

type qaSinkFunc func(context.Context, QATurnKey, string) (QATurnReceipt, error)

func (f qaSinkFunc) Commit(ctx context.Context, key QATurnKey, text string) (QATurnReceipt, error) {
	return f(ctx, key, text)
}

func TestQATurnGate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell transport fixture requires POSIX")
	}
	task := Task{ID: qaTaskID, Attempt: 1, WorkspaceID: "qa-workspace", RuntimeID: "qa-runtime"}
	for _, tc := range []string{"unconfigured", "ok", "error", "timeout", "late receipt", "wrong ID", "wrong attempt", "wrong hash", "no timestamp", "missing attempt"} {
		t.Run(tc, func(t *testing.T) {
			dir := t.TempDir()
			committed := filepath.Join(dir, "committed")
			turns := filepath.Join(dir, "turns")
			// The transport records the commit marker observed on receipt of the turn.
			script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "codex-cli 0.0.0-test"; exit 0; fi
read -r line || exit
echo '{"jsonrpc":"2.0","id":1,"result":{}}'
read -r line || exit
read -r line || exit
echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"qa-thread"}}}'
read -r line || exit
if [ -f "$QA_COMMITTED" ]; then echo committed >> "$QA_TURNS"; else echo uncommitted >> "$QA_TURNS"; fi
printf '%s\n' "$line" >> "$QA_TURNS"
echo '{"jsonrpc":"2.0","id":3,"result":{}}'
echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"qa-thread","turn":{"id":"qa-turn","status":"completed"}}}'
while read -r line; do
	case "$line" in *'"method":"turn/start"'*) printf '%s\n' "$line" >> "$QA_TURNS";; esac
done
`
			path := filepath.Join(dir, "codex")
			if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			backend, err := agent.New("codex", agent.Config{ExecutablePath: path, Logger: slog.Default(), Env: map[string]string{
				"QA_COMMITTED": committed, "QA_TURNS": turns,
			}})
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			const prompt = " exact QA input 한글 "

			sink := qaSinkFunc(func(ctx context.Context, key QATurnKey, text string) (QATurnReceipt, error) {
				calls++
				if text != prompt {
					t.Errorf("capture received different text: %q", text)
				}
				if _, err := os.Stat(turns); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("turn started before sink acknowledgment: %v", err)
				}
				r := QATurnReceipt{key, sha256.Sum256([]byte(text)), time.Now().UTC()}
				switch tc {
				case "ok":
					if err := os.WriteFile(committed, nil, 0o600); err != nil {
						return r, err
					}
				case "error":
					return r, errors.New("storage error")
				case "timeout":
					<-ctx.Done()
					return r, ctx.Err()
				case "late receipt":
					<-ctx.Done()
				case "wrong ID":
					r.TaskID = "other"
				case "wrong attempt":
					r.Attempt++
				case "wrong hash":
					r.SHA256 = [sha256.Size]byte{}
				case "no timestamp":
					r.StoredAtUTC = time.Time{}
				}
				return r, nil
			})
			config := Config{QATurnCapture: &QATurnCaptureConfig{WorkspaceID: task.WorkspaceID, RuntimeID: task.RuntimeID, Timeout: time.Second, Sink: sink}}
			if tc == "timeout" || tc == "late receipt" {
				config.QATurnCapture.Timeout = time.Millisecond
			}
			if config.qaTurnGate(task, "codex") != nil {
				t.Fatal("production profile enabled capture")
			}
			config.Profile = "nex1979-qa"
			if config.qaTurnGate(task, "claude") != nil || config.qaTurnGate(Task{WorkspaceID: task.WorkspaceID}, "codex") != nil {
				t.Fatal("out-of-scope task enabled capture")
			}
			candidate := task
			if tc == "missing attempt" {
				candidate.Attempt = 0
			}
			if tc == "unconfigured" {
				config.QATurnCapture = nil
			}
			session, err := backend.Execute(t.Context(), prompt, agent.ExecOptions{
				Timeout: 5 * time.Second, BeforeCodexTurn: config.qaTurnGate(candidate, "codex"),
			})
			if err != nil {
				t.Fatal(err)
			}
			for range session.Messages {
			}
			result := <-session.Result
			data, err := os.ReadFile(turns)
			if tc == "ok" || tc == "unconfigured" {
				marker := "committed"
				if tc == "unconfigured" {
					marker = "uncommitted"
				}
				lines := strings.Split(strings.TrimSpace(string(data)), "\n")
				if result.Status != "completed" || err != nil || len(lines) != 2 || lines[0] != marker {
					t.Fatalf("expected one turn after %s: result=%+v, data=%q, error=%v", marker, result, data, err)
				}
				var request struct {
					Method string
					Params struct {
						Input []struct{ Text string }
					}
				}
				if json.Unmarshal([]byte(lines[1]), &request) != nil || request.Method != "turn/start" ||
					len(request.Params.Input) != 1 || request.Params.Input[0].Text != prompt {
					t.Fatalf("unexpected first turn: %s", lines[1])
				}
			} else if result.Status != "failed" || !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("expected zero turns: result=%+v, data=%q, error=%v", result, data, err)
			}
			if (calls == 0) != (tc == "missing attempt" || tc == "unconfigured") || calls > 1 {
				t.Fatalf("unexpected sink calls: %d", calls)
			}
		})
	}
}

func TestQAPollerScope(t *testing.T) {
	d := Daemon{
		workspaces: map[string]*workspaceState{
			"qa":   {runtimeIDs: []string{"qa-codex", "qa-other"}},
			"prod": {runtimeIDs: []string{"prod-codex"}},
		},
		runtimeIndex: map[string]Runtime{
			"qa-codex": {Provider: "codex"}, "qa-other": {Provider: "claude"}, "prod-codex": {Provider: "codex"},
		},
	}
	if !d.qaAllowsPoller("prod-codex") {
		t.Fatal("unconfigured runtime polling changed")
	}
	d.cfg.Profile = "nex1979-qa"
	d.cfg.QATurnCapture = &QATurnCaptureConfig{WorkspaceID: "qa", RuntimeID: "qa-codex"}
	if !d.qaAllowsPoller("qa-codex") || d.qaAllowsPoller("prod-codex") || d.qaAllowsPoller("qa-other") {
		t.Fatal("QA poller is not scoped to the configured runtime")
	}
	d.cfg.QATurnCapture.RuntimeID = "prod-codex"
	if d.qaAllowsPoller("prod-codex") {
		t.Fatal("accepted a runtime from another workspace")
	}
	d.cfg.QATurnCapture.RuntimeID = "qa-other"
	if d.qaAllowsPoller("qa-other") {
		t.Fatal("accepted a non-Codex runtime")
	}
}
