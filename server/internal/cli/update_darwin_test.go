package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceBinaryPreservesAppBundle(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "Agent Host.app", "Contents", "MacOS", "multica")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(root, "candidate")
	for path, contents := range map[string]string{exe: "signed original", candidate: "unsigned update"} {
		if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := replaceBinary(candidate, exe); err == nil {
		t.Fatal("bundle executable replacement unexpectedly succeeded")
	}
	for path, want := range map[string]string{exe: "signed original", candidate: "unsigned update"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("%s: got %q, error %v; want %q", path, got, err, want)
		}
	}
}

func TestReplaceBinaryUpdatesStandalone(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "multica")
	candidate := filepath.Join(root, "candidate")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(candidate, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replaceBinary(candidate, exe); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(exe)
	if err != nil || string(got) != "new" {
		t.Fatalf("got %q, error %v; want new", got, err)
	}
}
