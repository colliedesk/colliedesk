package mcpserver_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
)

type funnelEvent struct {
	event, workspace string
	properties       map[string]any
}

// recordingFunnel stands in for PostHog: it keeps what the MCP server recorded.
type recordingFunnel struct {
	mu     sync.Mutex
	events []funnelEvent
}

func (f *recordingFunnel) Record(event, workspaceID string, properties map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, funnelEvent{event, workspaceID, properties})
}

func (f *recordingFunnel) take() []funnelEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	events := f.events
	f.events = nil
	return events
}

func newFunnelEnv(t *testing.T) (*env, *recordingFunnel) {
	funnel := &recordingFunnel{}
	return newEnvMCP(t, nil, nil, func(config *mcpserver.Config) { config.Funnel = funnel }), funnel
}

// wantOneToolCall checks the single mcp_tool_called event against values written out by hand.
func wantOneToolCall(t *testing.T, label string, events []funnelEvent, workspace string, want map[string]any) {
	t.Helper()
	if len(events) != 1 || events[0].event != "mcp_tool_called" {
		t.Fatalf("%s: recorded %+v, want exactly one mcp_tool_called", label, events)
	}
	if events[0].workspace != workspace {
		t.Errorf("%s: workspace = %q, want %q", label, events[0].workspace, workspace)
	}
	for key, value := range want {
		if events[0].properties[key] != value {
			t.Errorf("%s: %s = %v, want %v (%v)", label, key, events[0].properties[key], value, events[0].properties)
		}
	}
	if _, ok := events[0].properties["duration_ms"].(int64); !ok {
		t.Errorf("%s: duration_ms missing or not an int64: %v", label, events[0].properties)
	}
}

// A trial that signs up from an AI app works only through its tools, so the funnel must show which
// tools a workspace called and how each call ended, keyed by the workspace id like every funnel event.
func TestEveryToolCallIsRecordedInTheFunnelForItsWorkspace(t *testing.T) {
	e, funnel := newFunnelEnv(t)
	w := e.newWorkspace("Funnel Calls", "admin@funnel-calls.example.com")
	session := e.connectTraced(mcpserver.Path, w.read, "trace-funnel-001")
	funnel.take()

	e.ok(session, "colliedesk_whoami", nil)
	wantOneToolCall(t, "whoami", funnel.take(), w.id, map[string]any{"tool": "colliedesk_whoami", "token": "agent_token",
		"outcome": "ok", "error_type": ""})

	e.refused(session, "colliedesk_saved_reply_create", map[string]any{"title": "t", "body": "b"})
	wantOneToolCall(t, "refused", funnel.take(), w.id, map[string]any{"tool": "colliedesk_saved_reply_create", "token": "agent_token",
		"outcome": "refused", "error_type": "permission_missing"})
}

// An AI app's OAuth grant is told apart from an agent token a person made.
func TestToolCallFunnelEventNamesAnOAuthGrant(t *testing.T) {
	e, funnel := newFunnelEnv(t)
	w := e.newWorkspace("Funnel OAuth", "admin@funnel-oauth.example.com")
	session := e.connectTraced(mcpserver.Path, e.connectWithOAuth(w.admin), "trace-funnel-002")
	funnel.take()

	e.ok(session, "colliedesk_whoami", nil)
	wantOneToolCall(t, "oauth whoami", funnel.take(), w.id, map[string]any{"tool": "colliedesk_whoami", "token": "oauth", "outcome": "ok"})
}

// The event carries the tool and how it ended, never the arguments: they can hold a customer's words.
func TestToolCallFunnelEventNeverCarriesTheArguments(t *testing.T) {
	e, funnel := newFunnelEnv(t)
	w := e.newWorkspace("Funnel Private", "admin@funnel-private.example.com")
	session := e.connectTraced(mcpserver.Path, w.read, "trace-funnel-001")
	const marker = "okapi-marker-51c2e8"
	funnel.take()

	e.ok(session, "colliedesk_inbox_search", map[string]any{"q": marker})
	events := funnel.take()
	wantOneToolCall(t, "search", events, w.id, map[string]any{"tool": "colliedesk_inbox_search", "outcome": "ok"})
	if strings.Contains(fmt.Sprint(events[0].properties), marker) {
		t.Errorf("the event carries the arguments: %v", events[0].properties)
	}
}

// Operator tools run the whole server, not a workspace, so they never become a workspace's funnel event.
func TestOperatorToolCallsAreNotRecordedInTheFunnel(t *testing.T) {
	e, funnel := newFunnelEnv(t)
	session := e.connectTraced(mcpserver.OperatorPath, operatorToken(t, e), "trace-funnel-003")
	funnel.take()

	e.ok(session, "colliedesk_operator_whoami", nil)
	if events := funnel.take(); len(events) != 0 {
		t.Errorf("operator call recorded %+v, want nothing", events)
	}
}
