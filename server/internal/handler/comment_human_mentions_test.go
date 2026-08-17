package handler

import (
	"strings"
	"testing"
)

func TestHasHumanEscalationTag(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "plain mention", content: "[@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
		// Horizontal ASCII indentation is intentional; it does not hide the declaration.
		{name: "leading spaces before explicit tag are allowed", content: "  [협의체: P1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: true},
		{name: "leading tab before explicit tag is allowed", content: "\t[협의체: P1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: true},
		{name: "explicit p0 tag", content: "[협의체: P0] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: true},
		{name: "explicit p1 tag", content: "[협의체: P1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: true},
		{name: "explicit external cost tag", content: "[협의체: 외부비용] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: true},
		{name: "explicit prod db tag", content: "[협의체: PROD/DB] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: true},
		{name: "explicit external send tag", content: "[협의체: 외부발송] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: true},
		{name: "explicit public exposure tag", content: "[협의체: 라이선스/공개노출] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: true},
		{name: "uuid containing db is not a tag", content: "참조 mention://issue/ddf52e21-7511-46f3-a38c-5db83c45fab7 [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
		{name: "technical db prose is not a tag", content: "`data_cache.db`를 확인하세요 [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
		{name: "leading newline before tag is not a declaration", content: "\n[협의체: P1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
		{name: "newline inside tag is not a declaration", content: "[협의체:\nP1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
		{name: "fullwidth space before tag is not a declaration", content: "　[협의체: P1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
		{name: "zero width space before tag is not a declaration", content: "\u200b[협의체: P1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
		{name: "code block tag is not a declaration", content: "```\n[협의체: P1]\n``` [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
		{name: "blockquote tag is not a declaration", content: "> [협의체: P1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
		{name: "tag outside first line is not a declaration", content: "검토 요청\n[협의체: P1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasHumanEscalationTag(tt.content); got != tt.want {
				t.Fatalf("hasHumanEscalationTag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRewriteHumanMentions(t *testing.T) {
	content := "Please review [@Mib](mention://member/11111111-1111-1111-1111-111111111111) and [@Kim](mention://member/22222222-2222-2222-2222-222222222222)"

	t.Run("agent author without trigger tag is blocked and rerouted", func(t *testing.T) {
		got := rewriteHumanMentions(content, true)
		if strings.Contains(got, "mention://member/") {
			t.Fatalf("rewriteHumanMentions() kept a live member mention link: %q", got)
		}
		if !strings.Contains(got, "mention://agent/"+teamLeaderMentionUUID) {
			t.Fatalf("rewriteHumanMentions() did not reroute to TeamLeader: %q", got)
		}
		if !strings.Contains(got, "@Mib") || !strings.Contains(got, "@Kim") {
			t.Fatalf("rewriteHumanMentions() lost the original mentioned names: %q", got)
		}
	})

	t.Run("agent author with trigger tag passes through untouched", func(t *testing.T) {
		tagged := "[협의체: P1] " + content
		if got := rewriteHumanMentions(tagged, true); got != tagged {
			t.Fatalf("rewriteHumanMentions() with trigger tag = %q, want unchanged %q", got, tagged)
		}
	})

	t.Run("uuid and technical DB prose are blocked and rerouted", func(t *testing.T) {
		for _, content := range []string{
			"참조 mention://issue/ddf52e21-7511-46f3-a38c-5db83c45fab7 " + content,
			"`data_cache.db`를 확인하세요 " + content,
		} {
			got := rewriteHumanMentions(content, true)
			if strings.Contains(got, "mention://member/") || !strings.Contains(got, "mention://agent/"+teamLeaderMentionUUID) {
				t.Fatalf("rewriteHumanMentions() did not block incidental DB text: %q", got)
			}
		}
	})

	t.Run("newline-bypassed tags are blocked and rerouted", func(t *testing.T) {
		for _, content := range []string{
			"\n[협의체: P1] " + content,
			"[협의체:\nP1] " + content,
		} {
			got := rewriteHumanMentions(content, true)
			if strings.Contains(got, "mention://member/") || !strings.Contains(got, "mention://agent/"+teamLeaderMentionUUID) {
				t.Fatalf("rewriteHumanMentions() did not block a newline-bypassed tag: %q", got)
			}
		}
	})

	t.Run("human author is never rewritten, even without a trigger tag", func(t *testing.T) {
		if got := rewriteHumanMentions(content, false); got != content {
			t.Fatalf("rewriteHumanMentions() for human author = %q, want unchanged %q", got, content)
		}
	})

	t.Run("content without a member mention is untouched", func(t *testing.T) {
		plain := "no mentions here"
		if got := rewriteHumanMentions(plain, true); got != plain {
			t.Fatalf("rewriteHumanMentions() for mention-free content = %q, want unchanged %q", got, plain)
		}
	})
}
