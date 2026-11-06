package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func daemonProvisionOwnerTestCommand(outputFile string) *cobra.Command {
	cmd := &cobra.Command{Use: "provision-owner"}
	cmd.Flags().String("server-url", "", "")
	cmd.Flags().String("workspace-id", "workspace-123", "")
	cmd.Flags().String("daemon-id", "daemon-123", "")
	cmd.Flags().String("provider", "claude", "")
	cmd.Flags().String("name", "test runtime", "")
	cmd.Flags().String("device-name", "test host", "")
	cmd.Flags().String("output-file", outputFile, "")
	return cmd
}

func TestRunDaemonProvisionOwnerWritesTokenToPrivateFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-auth-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	const secret = "mdt_sensitive_one_time_token"
	path := filepath.Join(t.TempDir(), "daemon-token")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/daemon/provision-owner" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body["workspace_id"] != "workspace-123" || body["daemon_id"] != "daemon-123" {
			t.Fatalf("request body = %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"runtime_id": "runtime-456", "daemon_id": "daemon-123", "token": secret,
		})
	}))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	cmd := daemonProvisionOwnerTestCommand(path)
	stderr := captureStderr(t)
	out, err := captureStdout(t, func() error { return runDaemonProvisionOwner(cmd, nil) })
	errout := stderr.read()
	if err != nil {
		t.Fatalf("runDaemonProvisionOwner: %v", err)
	}
	if strings.Contains(out, secret) || strings.Contains(errout, secret) {
		t.Fatalf("raw token leaked; stdout=%q stderr=%q", out, errout)
	}
	for _, want := range []string{"Runtime ID: runtime-456", "Daemon ID: daemon-123", "Token ID: mdt_sen"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout %q does not contain %q", out, want)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if string(got) != secret {
		t.Fatalf("token file = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("file mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestRunDaemonProvisionOwnerRefusesExistingOutputFileBeforeAPI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-auth-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	path := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	cmd := daemonProvisionOwnerTestCommand(path)
	if err := runDaemonProvisionOwner(cmd, nil); err == nil {
		t.Fatal("runDaemonProvisionOwner succeeded with an existing file")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "keep" {
		t.Fatalf("existing file changed: %q, %v", got, err)
	}
	if requests != 0 {
		t.Fatalf("API requests = %d, want 0", requests)
	}
}

func TestRunDaemonProvisionOwnerRequiresOutputFileBeforeAPI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MULTICA_TOKEN", "test-auth-token")
	t.Setenv("MULTICA_WORKSPACE_ID", "workspace-123")
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	defer srv.Close()
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	cmd := daemonProvisionOwnerTestCommand("")
	err := runDaemonProvisionOwner(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--output-file is required") {
		t.Fatalf("error = %v, want --output-file required", err)
	}
	if requests != 0 {
		t.Fatalf("API requests = %d, want 0", requests)
	}
}
