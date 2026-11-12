package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Regression tests for the tenant-boundary findings F-01, F-03 and F-04 of
// the REA-2 isolation audit. Each test drives the vulnerable request shape
// and asserts the fixed (deny) behaviour plus the unchanged happy path.

// mintBoundaryTaskToken creates a real mat_ task token bound to
// testWorkspaceID and owned by testUserID, so requests can go through the
// production Auth middleware instead of hand-stamped headers.
func mintBoundaryTaskToken(t *testing.T) string {
	t.Helper()
	agentID := createHandlerTestAgent(t, "boundary-agent-"+t.Name(), nil)
	taskID := createHandlerTestTaskForAgent(t, agentID)
	raw, err := auth.GenerateAgentTaskToken()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testHandler.Queries.CreateTaskToken(context.Background(), db.CreateTaskTokenParams{
		TokenHash: auth.HashToken(raw), TaskID: parseUUID(taskID), AgentID: parseUUID(agentID),
		WorkspaceID: parseUUID(testWorkspaceID), UserID: parseUUID(testUserID),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}); err != nil {
		t.Fatal(err)
	}
	return raw
}

// seedForeignWorkspace creates a second workspace W2 that testUserID is ALSO
// a member of — the precondition for both F-01 and F-03.
func seedForeignWorkspace(t *testing.T, slug string) string {
	t.Helper()
	ctx := context.Background()
	var wsID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $1, '', 'FWB') RETURNING id::text`, slug).Scan(&wsID); err != nil {
		t.Fatalf("seed foreign workspace: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, wsID) })
	if _, err := testPool.Exec(ctx, `INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')`, wsID, testUserID); err != nil {
		t.Fatalf("seed foreign membership: %v", err)
	}
	return wsID
}

// F-01: a machine credential must not be able to mint a user JWT.
func TestIssueCliToken_RejectsMachineCredentials(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	raw := mintBoundaryTaskToken(t)
	req := httptest.NewRequest("POST", "/api/cli-token", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	w := httptest.NewRecorder()
	middleware.Auth(testHandler.Queries, nil, nil)(http.HandlerFunc(testHandler.IssueCliToken)).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("mat_ task token: status=%d want 403; body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "token\":\"ey") {
		t.Fatalf("JWT leaked to machine credential: %s", w.Body.String())
	}

	for _, src := range []string{"cloud_pat", "external_relay"} {
		req := httptest.NewRequest("POST", "/api/cli-token", nil)
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Actor-Source", src)
		w := httptest.NewRecorder()
		testHandler.IssueCliToken(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: status=%d want 403", src, w.Code)
		}
	}

	// Human session (JWT/mul_ PAT path leaves X-Actor-Source empty) still works.
	req = httptest.NewRequest("POST", "/api/cli-token", nil)
	req.Header.Set("X-User-ID", testUserID)
	w = httptest.NewRecorder()
	testHandler.IssueCliToken(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("human: status=%d want 200; body=%s", w.Code, w.Body.String())
	}
}

// F-03: attachment downloads must honour the task token's workspace binding.
func TestAttachmentDownload_TaskTokenBoundToWorkspace(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	store := &mockStorage{}
	origStorage, origCfg, origSigner := testHandler.Storage, testHandler.cfg, testHandler.CFSigner
	testHandler.Storage = store
	testHandler.cfg.AttachmentDownloadMode = "proxy"
	testHandler.CFSigner = nil
	t.Cleanup(func() {
		testHandler.Storage, testHandler.cfg, testHandler.CFSigner = origStorage, origCfg, origSigner
	})

	ctx := context.Background()
	foreignWS := seedForeignWorkspace(t, "f03-foreign-ws")
	raw := mintBoundaryTaskToken(t)

	seed := func(wsID, key, body string) string {
		store.put(key, []byte(body))
		store.put("/uploads/"+key, []byte(body))
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO attachment (workspace_id, uploader_type, uploader_id, filename, url, content_type, size_bytes)
			VALUES ($1, 'member', $2, 'f03.txt', $3, 'text/plain', $4) RETURNING id::text`,
			wsID, testUserID, "/uploads/"+key, len(body)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM attachment WHERE id = $1`, id) })
		return id
	}
	foreignID := seed(foreignWS, "f03/foreign.txt", "foreign-secret")
	ownID := seed(testWorkspaceID, "f03/own.txt", "own-bytes")

	router := chi.NewRouter()
	router.Get("/api/attachments/{id}/download", testHandler.DownloadAttachment)
	router.Get("/uploads/*", testHandler.ServeLocalUpload)
	authed := middleware.Auth(testHandler.Queries, nil, nil)(router)

	do := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		w := httptest.NewRecorder()
		authed.ServeHTTP(w, req)
		return w
	}

	for _, path := range []string{"/api/attachments/" + foreignID + "/download", "/uploads/f03/foreign.txt"} {
		w := do(path)
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "foreign-secret") {
			t.Fatalf("%s cross-workspace: status=%d body=%q, want 404", path, w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/api/attachments/" + ownID + "/download", "/uploads/f03/own.txt"} {
		w := do(path)
		if w.Code != http.StatusOK || w.Body.String() != "own-bytes" {
			t.Fatalf("%s bound workspace: status=%d body=%q, want 200", path, w.Code, w.Body.String())
		}
	}

	// A human session in both workspaces still downloads the W2 attachment.
	req := httptest.NewRequest("GET", "/api/attachments/"+foreignID+"/download", nil)
	req.Header.Set("X-User-ID", testUserID)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("human cross-workspace member: status=%d want 200", w.Code)
	}
}

// F-04: issue update paths must reject a project from another workspace.
func TestUpdateIssue_RejectsForeignWorkspaceProject(t *testing.T) {
	if testPool == nil {
		t.Skip("test database not available")
	}
	ctx := context.Background()
	var foreignWS string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('F04 Foreign', 'f04-foreign-ws', '', 'FFF') RETURNING id::text`).Scan(&foreignWS); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace WHERE id = $1`, foreignWS) })
	project := func(wsID, title string) string {
		var id string
		if err := testPool.QueryRow(ctx, `INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id::text`, wsID, title).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM project WHERE id = $1`, id) })
		return id
	}
	foreignProject := project(foreignWS, "F04 foreign project")
	ownProject := project(testWorkspaceID, "F04 own project")

	issueID := createTestIssue(t, "F04 issue", "todo", "low")
	t.Cleanup(func() { deleteTestIssue(t, issueID) })

	projectOf := func() string {
		var p *string
		if err := testPool.QueryRow(ctx, `SELECT project_id::text FROM issue WHERE id = $1`, issueID).Scan(&p); err != nil {
			t.Fatal(err)
		}
		if p == nil {
			return ""
		}
		return *p
	}
	update := func(projectID string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{"project_id": projectID}), "id", issueID)
		testHandler.UpdateIssue(w, req)
		return w
	}
	batch := func(projectID string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		testHandler.BatchUpdateIssues(w, newRequest("POST", "/api/issues/batch-update", map[string]any{
			"issue_ids": []string{issueID},
			"updates":   map[string]any{"project_id": projectID},
		}))
		return w
	}

	if w := update(foreignProject); w.Code != http.StatusBadRequest {
		t.Fatalf("PUT foreign project: status=%d want 400; body=%s", w.Code, w.Body.String())
	}
	if w := batch(foreignProject); w.Code != http.StatusBadRequest {
		t.Fatalf("batch foreign project: status=%d want 400; body=%s", w.Code, w.Body.String())
	}
	if got := projectOf(); got != "" {
		t.Fatalf("foreign project was linked: %s", got)
	}

	if w := update(ownProject); w.Code != http.StatusOK {
		t.Fatalf("PUT own project: status=%d; body=%s", w.Code, w.Body.String())
	}
	if got := projectOf(); got != ownProject {
		t.Fatalf("own project not linked: %q", got)
	}
	w := batch(ownProject)
	var resp struct {
		Updated int `json:"updated"`
	}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if w.Code != http.StatusOK || resp.Updated != 1 {
		t.Fatalf("batch own project: status=%d updated=%d", w.Code, resp.Updated)
	}
	// Explicit null still clears the project.
	w = httptest.NewRecorder()
	testHandler.UpdateIssue(w, withURLParam(newRequest("PUT", "/api/issues/"+issueID, map[string]any{"project_id": nil}), "id", issueID))
	if w.Code != http.StatusOK || projectOf() != "" {
		t.Fatalf("clear project: status=%d project=%q", w.Code, projectOf())
	}
}
