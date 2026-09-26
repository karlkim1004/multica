package service

import (
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"testing"
)

func TestAutopilotChatContent(t *testing.T) {
	for _, tc := range []struct{ status, result, want string }{
		{"completed", `{"output":"hello"}`, "hello"},
		{"completed", `{"output":"  "}`, ""},
		{"completed", `{}`, ""},
		{"completed", `bad-json`, ""},
		{"failed", `{"output":"secret diagnostics"}`, "자동 보고 작업이 완료되지 않았습니다. 실행 기록에서 실패 또는 취소 상태를 확인해 주세요."},
		{"cancelled", `{}`, "자동 보고 작업이 완료되지 않았습니다. 실행 기록에서 실패 또는 취소 상태를 확인해 주세요."},
	} {
		if got := autopilotChatContent(db.AgentTaskQueue{Status: tc.status, Result: []byte(tc.result)}); got != tc.want {
			t.Errorf("%s %s: got %q want %q", tc.status, tc.result, got, tc.want)
		}
	}
}
