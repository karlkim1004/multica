//go:build nex1979_qa_capture

package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/daemon"
)

func configureQATurnCapture(cfg *daemon.Config) error {
	if cfg.Profile != "nex1979-qa" {
		return errors.New("QA capture binary requires the nex1979-qa profile")
	}
	dir := os.Getenv("MULTICA_QA_CAPTURE_DIR")
	ws := os.Getenv("MULTICA_QA_CAPTURE_WORKSPACE_ID")
	runtime := os.Getenv("MULTICA_QA_CAPTURE_RUNTIME_ID")
	if !filepath.IsAbs(dir) || !canonicalQAID(ws) || !canonicalQAID(runtime) {
		return errors.New("QA capture requires an absolute sink path and canonical workspace/runtime IDs")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("QA capture requires an existing private mode 0700 sink directory")
	}
	cfg.AutoUpdateEnabled = false
	cfg.QATurnCapture = &daemon.QATurnCaptureConfig{
		WorkspaceID: ws, RuntimeID: runtime, Timeout: 5 * time.Second,
		Sink: daemon.QAFileTurnSink(dir),
	}
	return nil
}

func canonicalQAID(s string) bool {
	id, err := uuid.Parse(s)
	return err == nil && id != uuid.Nil && id.String() == s
}

func init() {
	daemonCmd.AddCommand(&cobra.Command{
		Use:    "qa-verify-capture <capture.json> <official-result.json> <official-baseline.json>",
		Short:  "Verify a QA capture against read-only official task records",
		Hidden: true,
		Args:   cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			capture, err := os.Open(args[0])
			if err != nil {
				return err
			}
			defer capture.Close()
			result, err := os.Open(args[1])
			if err != nil {
				return err
			}
			defer result.Close()
			baseline, err := os.Open(args[2])
			if err != nil {
				return err
			}
			defer baseline.Close()
			return daemon.VerifyQACompletion(capture, result, baseline)
		},
	})
}
