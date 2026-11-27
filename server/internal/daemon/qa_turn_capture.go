package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// QATurnCaptureConfig selects only the approved QA Codex runtime.
type QATurnCaptureConfig struct {
	WorkspaceID string
	RuntimeID   string
	Timeout     time.Duration
	Sink        QATurnSink
}

type QATurnKey struct {
	TaskID  string `json:"id"`
	Attempt int32  `json:"attempt"`
}

type QATurnReceipt struct {
	QATurnKey
	SHA256      [sha256.Size]byte
	StoredAtUTC time.Time
}

type QATurnSink interface {
	// Commit acknowledges a durable record and must honor context cancellation.
	Commit(ctx context.Context, key QATurnKey, text string) (QATurnReceipt, error)
}

func (d *Daemon) qaAllowsPoller(runtimeID string) bool {
	qa := d.cfg.QATurnCapture
	if qa == nil {
		return true
	}
	if d.cfg.Profile != "nex1979-qa" || runtimeID != qa.RuntimeID {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	ws := d.workspaces[qa.WorkspaceID]
	return ws != nil && slices.Contains(ws.runtimeIDs, runtimeID) &&
		d.runtimeIndex[runtimeID].Provider == "codex"
}

func (c *Config) qaTurnGate(task Task, provider string) func(context.Context, string) error {
	qa := c.QATurnCapture
	if qa == nil || c.Profile != "nex1979-qa" || provider != "codex" ||
		task.WorkspaceID != qa.WorkspaceID || task.RuntimeID != qa.RuntimeID {
		return nil
	}
	key := QATurnKey{TaskID: task.ID, Attempt: task.Attempt}
	return func(ctx context.Context, text string) error {
		if !key.valid() || !utf8.ValidString(text) || qa.Timeout <= 0 || qa.Sink == nil {
			return errors.New("invalid QA capture configuration or task identity")
		}
		ctx, cancel := context.WithTimeout(ctx, qa.Timeout)
		defer cancel()
		receipt, err := qa.Sink.Commit(ctx, key, text)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		_, offset := receipt.StoredAtUTC.Zone()
		if receipt.QATurnKey != key || receipt.SHA256 != sha256.Sum256([]byte(text)) ||
			receipt.StoredAtUTC.IsZero() || offset != 0 {
			return errors.New("QA capture receipt mismatch")
		}
		return nil
	}
}

func (k QATurnKey) valid() bool {
	id, err := uuid.Parse(k.TaskID)
	return err == nil && id != uuid.Nil && id.String() == k.TaskID && k.Attempt > 0
}

type QACompletedTask struct {
	QATurnKey
	Status string `json:"status"`
}

// matchQACompletion checks identities from official read-only result and
// baseline lookups before the collector associates either with captured input.
func matchQACompletion(receipt QATurnReceipt, result, baseline QACompletedTask) bool {
	return receipt.QATurnKey.valid() &&
		result.Status == "completed" && baseline.Status == "completed" &&
		result.QATurnKey == receipt.QATurnKey && baseline.QATurnKey == receipt.QATurnKey
}

// VerifyQACompletion verifies a stored capture against two official task
// records supplied by the caller through a read-only lookup.
func VerifyQACompletion(capture, result, baseline io.Reader) error {
	var record qaTurnRecord
	var officialResult, officialBaseline QACompletedTask
	if json.NewDecoder(capture).Decode(&record) != nil ||
		record.StoredAtUTC.IsZero() || record.SHA256 != sha256.Sum256([]byte(record.Text)) {
		return errors.New("invalid QA capture record")
	}
	if json.NewDecoder(result).Decode(&officialResult) != nil ||
		json.NewDecoder(baseline).Decode(&officialBaseline) != nil ||
		!matchQACompletion(record.QATurnReceipt, officialResult, officialBaseline) {
		return errors.New("official result or baseline does not match the completed task attempt")
	}
	return nil
}

// QAFileTurnSink stores immutable input records in an existing, dedicated
// private directory on a local filesystem supporting hard links and fsync.
type QAFileTurnSink string

type qaTurnRecord struct {
	QATurnReceipt
	Text string
}

func (sink QAFileTurnSink) Commit(ctx context.Context, key QATurnKey, text string) (QATurnReceipt, error) {
	var empty QATurnReceipt
	if !key.valid() || !utf8.ValidString(text) || !filepath.IsAbs(string(sink)) {
		return empty, errors.New("invalid QA sink path or input")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	root, err := os.OpenRoot(string(sink))
	if err != nil {
		return empty, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return empty, err
	}
	defer dir.Close()
	info, err := dir.Stat()
	if err != nil {
		return empty, err
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return empty, errors.New("QA sink requires a private mode 0700 directory")
	}

	record := qaTurnRecord{
		QATurnReceipt: QATurnReceipt{
			QATurnKey: key, SHA256: sha256.Sum256([]byte(text)), StoredAtUTC: time.Now().UTC(),
		},
		Text: text,
	}
	name := fmt.Sprintf("%s-%d.json", key.TaskID, key.Attempt)
	tmp := ".capture-" + uuid.NewString()
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return empty, err
	}
	defer root.Remove(tmp)
	writeErr := json.NewEncoder(f).Encode(record)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	if err := errors.Join(writeErr, f.Close()); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := root.Link(tmp, name); errors.Is(err, fs.ErrExist) {
		data, err := root.ReadFile(name)
		if err != nil {
			return empty, err
		}
		var existing qaTurnRecord
		if err := json.Unmarshal(data, &existing); err != nil ||
			existing.QATurnKey != key || existing.SHA256 != record.SHA256 ||
			existing.Text != text || existing.StoredAtUTC.IsZero() {
			return empty, errors.New("QA capture conflict or invalid stored record")
		}
		record = existing
	} else if err != nil {
		return empty, err
	}
	if err := dir.Sync(); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return record.QATurnReceipt, nil
}
