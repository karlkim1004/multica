package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCommentRedactionPreservesUUID(t *testing.T) {
	const (
		eventID     = "c21586fa-db6a-407e-8b9a-0101234567df"
		phone       = "010-5555-0123"
		maskedPhone = "*******0123"
	)
	t.Setenv("PII_REDACT_ALLOWLIST", "")
	t.Setenv("PII_REDACT_MAPPING", t.TempDir()+"/mapping.json")

	issueID := createIssue(t, "Comment redaction integration test")
	t.Cleanup(func() {
		resp := authRequest(t, http.MethodDelete, "/api/issues/"+issueID, nil)
		resp.Body.Close()
	})

	decision := fmt.Sprintf("{\"schema_version\":\"1.0\",\"event_id\":%q,\"issue_id\":%q,\"producer_id\":\"a9b0fb13-bfaf-4cea-a6e4-d27e243ec2b0\",\"producer_role\":\"router\",\"decision\":\"WAIT\",\"expected_updated_at\":\"2026-08-11T10:00:00Z\",\"evidence\":%q}", eventID, issueID, "전화 "+phone)
	for _, tc := range []struct {
		name, content string
	}{
		{"chain_decision", "```chain_decision\n" + decision + "\n```"},
		{"plain_text", "연락처 " + phone + " / 이벤트 " + eventID + " / 처리 대기 중"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.ReplaceAll(tc.content, phone, maskedPhone)
			commentID := postComment(t, issueID, tc.content, nil)
			assertStored := func(t *testing.T, want string) {
				t.Helper()
				var stored string
				if err := testPool.QueryRow(context.Background(), "SELECT content FROM comment WHERE id = $1", commentID).Scan(&stored); err != nil {
					t.Fatal(err)
				}
				if tc.name == "chain_decision" {
					payload := strings.Split(stored, "\n")[1]
					t.Logf("stored decision: %s", payload)
					var parsed struct {
						EventID string `json:"event_id"`
					}
					if err := json.Unmarshal([]byte(payload), &parsed); err != nil || parsed.EventID != eventID {
						t.Errorf("stored event_id = %q, parse error = %v", parsed.EventID, err)
					}
				}
				if stored != want {
					t.Errorf("stored content = %q, want %q", stored, want)
				}
			}
			t.Run("create", func(t *testing.T) { assertStored(t, want) })
			t.Run("edit", func(t *testing.T) {
				resp := authRequest(t, http.MethodPut, "/api/comments/"+commentID, map[string]any{
					"content": tc.content + "\n수정했습니다.",
				})
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					body, _ := io.ReadAll(resp.Body)
					t.Fatalf("update status = %d: %s", resp.StatusCode, body)
				}
				assertStored(t, want+"\n수정했습니다.")
			})
		})
	}
}
