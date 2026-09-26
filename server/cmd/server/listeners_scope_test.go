package main

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestRegisterListeners_PrivateReportOnlyReachesRecipient(t *testing.T) {
	bus := events.New()
	fb := &fakeBroadcaster{}
	registerListeners(bus, fb)
	const report = "private financial report: account balance 12345"
	bus.Publish(events.Event{
		Type: protocol.EventChatMessage, WorkspaceID: "shared-workspace",
		ActorType: "agent", ActorID: "report-agent", ChatSessionID: "private-chat",
		RecipientUserID: "report-owner",
		Payload:         protocol.ChatMessagePayload{ChatSessionID: "private-chat", MessageID: "report-message", Role: "assistant", Content: report},
	})
	if len(fb.workspaceCalls) != 0 || len(fb.scopeCalls) != 0 || fb.broadcastCalled != 0 {
		t.Fatalf("private report escaped recipient routing: workspace=%d scope=%d global=%d", len(fb.workspaceCalls), len(fb.scopeCalls), fb.broadcastCalled)
	}
	if len(fb.userCalls) != 1 || fb.userCalls[0].userID != "report-owner" {
		t.Fatalf("report must reach only its owner: %+v", fb.userCalls)
	}
	if len(fb.userCalls[0].exclude) != 0 {
		t.Fatal("owner's active workspace connection must receive the report")
	}
	var received struct {
		Type    string                      `json:"type"`
		Payload protocol.ChatMessagePayload `json:"payload"`
	}
	if err := json.Unmarshal(fb.userCalls[0].msg, &received); err != nil {
		t.Fatal(err)
	}
	if received.Type != protocol.EventChatMessage || received.Payload.Content != report || received.Payload.ChatSessionID != "private-chat" {
		t.Fatalf("owner did not receive the complete report: %+v", received)
	}
}

// fakeBroadcaster records every fanout call so tests can assert which scope a
// given event landed on.
type fakeBroadcaster struct {
	mu              sync.Mutex
	scopeCalls      []scopeCall
	workspaceCalls  []workspaceCall
	userCalls       []userCall
	broadcastCalled int
}

type scopeCall struct {
	scopeType, scopeID string
	msg                []byte
}
type workspaceCall struct {
	workspaceID string
	msg         []byte
}
type userCall struct {
	userID  string
	msg     []byte
	exclude []string
}

func (f *fakeBroadcaster) BroadcastToScope(scopeType, scopeID string, message []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopeCalls = append(f.scopeCalls, scopeCall{scopeType, scopeID, message})
}
func (f *fakeBroadcaster) BroadcastToWorkspace(workspaceID string, message []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.workspaceCalls = append(f.workspaceCalls, workspaceCall{workspaceID, message})
}
func (f *fakeBroadcaster) SendToUser(userID string, message []byte, excludeWorkspace ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.userCalls = append(f.userCalls, userCall{userID, message, excludeWorkspace})
}
func (f *fakeBroadcaster) Broadcast(message []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.broadcastCalled++
}

// TestRegisterListeners_TaskChatGoToWorkspace pins the must-fix #1 contract
// from the PR #1429 review: until the WS client supports scope-subscribe and
// reconnect-replay, high-frequency task/chat events MUST keep going through
// workspace fanout. Routing them via BroadcastToScope("task"|"chat", ...)
// with no client-side subscriber would silently drop every chat / task
// message and break the live timeline + chat unread badges.
func TestRegisterListeners_TaskChatGoToWorkspace(t *testing.T) {
	cases := []struct {
		name      string
		eventType string
		taskID    string
		chatID    string
	}{
		{"task:message with TaskID", protocol.EventTaskMessage, "task-1", ""},
		{"task:progress with TaskID", protocol.EventTaskProgress, "task-2", ""},
		{"chat:message with ChatSessionID", protocol.EventChatMessage, "", "chat-1"},
		{"chat:done with ChatSessionID", protocol.EventChatDone, "", "chat-2"},
		{"chat:session_read with ChatSessionID", protocol.EventChatSessionRead, "", "chat-3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bus := events.New()
			fb := &fakeBroadcaster{}
			registerListeners(bus, fb)

			bus.Publish(events.Event{
				Type:          tc.eventType,
				WorkspaceID:   "ws-1",
				TaskID:        tc.taskID,
				ChatSessionID: tc.chatID,
				Payload:       map[string]any{"hello": "world"},
			})

			if len(fb.scopeCalls) != 0 {
				t.Fatalf("expected no BroadcastToScope calls (must-fix #1: keep workspace fanout until client lands), got %+v", fb.scopeCalls)
			}
			if len(fb.workspaceCalls) != 1 {
				t.Fatalf("expected exactly 1 BroadcastToWorkspace call, got %d", len(fb.workspaceCalls))
			}
			if fb.workspaceCalls[0].workspaceID != "ws-1" {
				t.Fatalf("expected workspace ws-1, got %q", fb.workspaceCalls[0].workspaceID)
			}
		})
	}
}
