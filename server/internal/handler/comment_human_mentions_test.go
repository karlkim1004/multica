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
		{name: "p1 tag", content: "P1 [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: true},
		{name: "external cost tag", content: "외부비용 [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: true},
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
		tagged := "P1 " + content
		if got := rewriteHumanMentions(tagged, true); got != tagged {
			t.Fatalf("rewriteHumanMentions() with trigger tag = %q, want unchanged %q", got, tagged)
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
