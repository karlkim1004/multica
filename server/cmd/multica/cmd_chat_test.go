package main

import (
	"github.com/spf13/cobra"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChatReadUsesRegisteredRoutes(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		path string
	}{
		{"list", nil, "/api/chat/sessions"},
		{"get", []string{"session"}, "/api/chat/sessions/session"},
		{"messages", []string{"session"}, "/api/chat/sessions/session/messages"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				if r.Method != "GET" || r.URL.Path != tc.path {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(404)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			t.Setenv("MULTICA_SERVER_URL", srv.URL)
			t.Setenv("MULTICA_WORKSPACE_ID", "ws-test")
			t.Setenv("MULTICA_TOKEN", "test-token")
			cmd := &cobra.Command{Use: tc.name}
			if err := runChatRead(cmd, tc.args); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("no read request")
			}
		})
	}
}
