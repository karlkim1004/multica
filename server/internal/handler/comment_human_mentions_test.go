package handler

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestHasHumanEscalationTag(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "plain mention", content: "[@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
		// Up to three leading ASCII spaces preserve a visible declaration without creating a Markdown code block.
		{name: "leading spaces before explicit tag are allowed", content: "  [협의체: P1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: true},
		{name: "three leading spaces before explicit tag are allowed", content: "   [협의체: P1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: true},
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
		{name: "leading tab before tag is not a declaration", content: "\t[협의체: P1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
		{name: "four leading spaces before tag is not a declaration", content: "    [협의체: P1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
		{name: "tab inside tag is not a declaration", content: "[협의체:\tP1] [@Mib](mention://member/11111111-1111-1111-1111-111111111111)", want: false},
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
		got := rewriteHumanMentions(content, true, teamLeaderMentionUUID, "아이유(TeamLeader)", "default_teamleader")
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
		if got := rewriteHumanMentions(tagged, true, teamLeaderMentionUUID, "아이유(TeamLeader)", "default_teamleader"); got != tagged {
			t.Fatalf("rewriteHumanMentions() with trigger tag = %q, want unchanged %q", got, tagged)
		}
	})

	t.Run("uuid and technical DB prose are blocked and rerouted", func(t *testing.T) {
		for _, content := range []string{
			"참조 mention://issue/ddf52e21-7511-46f3-a38c-5db83c45fab7 " + content,
			"`data_cache.db`를 확인하세요 " + content,
		} {
			got := rewriteHumanMentions(content, true, teamLeaderMentionUUID, "아이유(TeamLeader)", "default_teamleader")
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
			got := rewriteHumanMentions(content, true, teamLeaderMentionUUID, "아이유(TeamLeader)", "default_teamleader")
			if strings.Contains(got, "mention://member/") || !strings.Contains(got, "mention://agent/"+teamLeaderMentionUUID) {
				t.Fatalf("rewriteHumanMentions() did not block a newline-bypassed tag: %q", got)
			}
		}
	})

	t.Run("human author is never rewritten, even without a trigger tag", func(t *testing.T) {
		if got := rewriteHumanMentions(content, false, teamLeaderMentionUUID, "아이유(TeamLeader)", "default_teamleader"); got != content {
			t.Fatalf("rewriteHumanMentions() for human author = %q, want unchanged %q", got, content)
		}
	})

	t.Run("content without a member mention is untouched", func(t *testing.T) {
		plain := "no mentions here"
		if got := rewriteHumanMentions(plain, true, teamLeaderMentionUUID, "아이유(TeamLeader)", "default_teamleader"); got != plain {
			t.Fatalf("rewriteHumanMentions() for mention-free content = %q, want unchanged %q", got, plain)
		}
	})
}

func TestResolveRerouteTarget(t *testing.T) {
	agentID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	memberID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	agentIDString := uuidToString(agentID)
	tests := []struct {
		name, authorType, authorID, wantUUID, wantReason string
		issue                                            db.Issue
	}{
		{"blocked agent", "agent", "other", chainKeeperMentionUUID, "blocked_status", db.Issue{Status: "blocked", AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: agentID}},
		{"blocked no assignee", "agent", "other", chainKeeperMentionUUID, "blocked_status", db.Issue{Status: "blocked"}},
		{"todo other agent", "agent", "other", agentIDString, "issue_assignee", db.Issue{Status: "todo", AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: agentID}},
		{"todo self agent", "agent", agentIDString, teamLeaderMentionUUID, "default_teamleader", db.Issue{Status: "todo", AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: agentID}},
		{"todo member", "agent", "other", teamLeaderMentionUUID, "default_teamleader", db.Issue{Status: "todo", AssigneeType: pgtype.Text{String: "member", Valid: true}, AssigneeID: memberID}},
		{"todo no assignee", "agent", "other", teamLeaderMentionUUID, "default_teamleader", db.Issue{Status: "todo"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUUID, _, gotReason := resolveRerouteTarget(tt.issue, tt.authorType, tt.authorID)
			if gotUUID != tt.wantUUID || gotReason != tt.wantReason {
				t.Fatalf("resolveRerouteTarget() = (%q, %q), want (%q, %q)", gotUUID, gotReason, tt.wantUUID, tt.wantReason)
			}
		})
	}
}

func TestRewriteHumanMentionsUsesResolvedTarget(t *testing.T) {
	content := "[@Mib](mention://member/11111111-1111-1111-1111-111111111111)"
	assigneeID := pgtype.UUID{Bytes: [16]byte{9}, Valid: true}
	for _, tt := range []struct {
		name, authorID, wantUUID string
		issue                    db.Issue
	}{
		{"blocked uses chain keeper", "author", chainKeeperMentionUUID, db.Issue{Status: "blocked"}},
		{"other assignee uses assignee", "author", uuidToString(assigneeID), db.Issue{Status: "todo", AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: assigneeID}},
		{"self assignee uses team leader", uuidToString(assigneeID), teamLeaderMentionUUID, db.Issue{Status: "todo", AssigneeType: pgtype.Text{String: "agent", Valid: true}, AssigneeID: assigneeID}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			uuid, label, reason := resolveRerouteTarget(tt.issue, "agent", tt.authorID)
			got := rewriteHumanMentions(content, true, uuid, label, reason)
			if strings.Contains(got, "mention://member/") || !strings.Contains(got, "mention://agent/"+tt.wantUUID) {
				t.Fatalf("rewriteHumanMentions() route = %q, want agent %s", got, tt.wantUUID)
			}
		})
	}
	if got := rewriteHumanMentions("[협의체: P1] "+content, true, chainKeeperMentionUUID, "민주(chain-keeper)", "blocked_status"); got != "[협의체: P1] "+content {
		t.Fatalf("tagged mention was rewritten: %q", got)
	}
}
