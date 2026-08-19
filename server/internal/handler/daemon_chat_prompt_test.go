package handler

import (
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func msg(role, content string) db.ChatMessage {
	return db.ChatMessage{Role: role, Content: content}
}

func contents(msgs []db.ChatMessage) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Content
	}
	return out
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestTrailingUserMessages pins the message-selection logic behind the daemon
// chat prompt: the agent must receive every user message since its last reply
// (the MUL-2968 debounce can land several before one run fires), not just the
// most recent one.
func TestTrailingUserMessages(t *testing.T) {
	cases := []struct {
		name string
		in   []db.ChatMessage
		want []string
	}{
		{
			name: "debounced burst with no prior reply delivers all",
			in:   []db.ChatMessage{msg("user", "看上海天气"), msg("user", "还有青岛")},
			want: []string{"看上海天气", "还有青岛"},
		},
		{
			name: "only messages after the last assistant reply",
			in: []db.ChatMessage{
				msg("user", "old q"), msg("assistant", "old a"),
				msg("user", "看上海天气"), msg("user", "还有青岛"),
			},
			want: []string{"看上海天气", "还有青岛"},
		},
		{
			name: "single new message after a reply",
			in: []db.ChatMessage{
				msg("user", "看上海天气"), msg("user", "还有青岛"),
				msg("assistant", "weather…"), msg("user", "深圳呢"),
			},
			want: []string{"深圳呢"},
		},
		{
			name: "no trailing user message (last is assistant)",
			in:   []db.ChatMessage{msg("user", "hi"), msg("assistant", "done")},
			want: []string{},
		},
		{
			name: "empty history",
			in:   []db.ChatMessage{},
			want: []string{},
		},
		{
			name: "single user message",
			in:   []db.ChatMessage{msg("user", "hi")},
			want: []string{"hi"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := contents(trailingUserMessages(tc.in))
			if !eq(got, tc.want) {
				t.Fatalf("trailingUserMessages = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestBuildChatHistoryText pins the NEX-964 fix: when a chat task has no
// native session to resume (runtime switch, cold start after a failed
// turn, etc.), the daemon reconstructs the prior conversation from
// chat_message rows so the agent doesn't start from zero. This function
// renders those rows into the "Role: content" transcript injected into the
// prompt.
func TestBuildChatHistoryText(t *testing.T) {
	t.Run("empty history yields empty text", func(t *testing.T) {
		if got := buildChatHistoryText(nil); got != "" {
			t.Fatalf("buildChatHistoryText(nil) = %q, want empty", got)
		}
	})

	t.Run("renders roles and preserves order", func(t *testing.T) {
		in := []db.ChatMessage{msg("user", "my favorite color is blue"), msg("assistant", "got it, blue.")}
		got := buildChatHistoryText(in)
		want := "User: my favorite color is blue\nAssistant: got it, blue."
		if got != want {
			t.Fatalf("buildChatHistoryText = %q, want %q", got, want)
		}
	})

	t.Run("skips blank messages", func(t *testing.T) {
		in := []db.ChatMessage{msg("user", "  "), msg("user", "hi"), msg("assistant", "")}
		got := buildChatHistoryText(in)
		if got != "User: hi" {
			t.Fatalf("buildChatHistoryText = %q, want %q", got, "User: hi")
		}
	})

	t.Run("truncates to the char budget, keeping the most recent messages and marking the cut", func(t *testing.T) {
		in := make([]db.ChatMessage, 0, 2000)
		for i := 0; i < 2000; i++ {
			in = append(in, msg("user", "x"))
		}
		in = append(in, msg("assistant", "the most recent reply"))
		got := buildChatHistoryText(in)
		if len(got) > chatHistoryCharBudget+len("[earlier messages omitted for length]\n") {
			t.Fatalf("buildChatHistoryText result too long: %d chars", len(got))
		}
		if !strings.HasPrefix(got, "[earlier messages omitted for length]\n") {
			t.Fatalf("expected truncation marker, got prefix %q", got[:min(60, len(got))])
		}
		if !strings.HasSuffix(got, "Assistant: the most recent reply") {
			t.Fatalf("expected the most recent message to survive truncation, got suffix %q", got[max(0, len(got)-60):])
		}
	})
}
