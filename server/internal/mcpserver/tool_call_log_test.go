package mcpserver_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// traced sends the bearer token and a fixed X-Request-Id, the way a reverse proxy in front of the
// server names a request, so a test knows which id the log line must carry.
type traced struct{ token, requestID string }

func (t traced) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	req.Header.Set("X-Request-Id", t.requestID)
	return http.DefaultTransport.RoundTrip(req)
}

func (e *env) connectTraced(path, token, requestID string) *mcp.ClientSession {
	e.t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "colliedesk-test", Version: "0"}, nil)
	transport := &mcp.StreamableClientTransport{Endpoint: e.server.URL + path, HTTPClient: &http.Client{Transport: traced{token, requestID}},
		DisableStandaloneSSE: true, MaxRetries: -1}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		e.t.Fatalf("connect: %v", err)
	}
	e.t.Cleanup(func() { _ = session.Close() })
	return session
}

// toolCallLine is the one "mcp tool call" line the buffer must hold since it was last reset.
func toolCallLine(t *testing.T, logs *bytes.Buffer, label string) map[string]any {
	t.Helper()
	lines := logLines(t, logs, "mcp tool call")
	if len(lines) != 1 {
		t.Fatalf("%s: %d \"mcp tool call\" lines, want exactly one:\n%s", label, len(lines), logs.String())
	}
	return lines[0]
}

// wantFields checks the fields of a log line against the values written out by hand.
func wantFields(t *testing.T, label string, line map[string]any, want map[string]any) {
	t.Helper()
	for key, value := range want {
		if line[key] != value {
			t.Errorf("%s: %s = %v, want %v (line %v)", label, key, line[key], value, line)
		}
	}
	if _, ok := line["duration_ms"].(float64); !ok {
		t.Errorf("%s: duration_ms missing or not a number: %v", label, line)
	}
}

// Every tool call leaves one line naming the workspace, the tool, the kind of token, how it ended and
// the request id, so the team can see what an AI app did in a workspace and why a call failed. The
// arguments never reach the log: they can hold a customer's words.
func TestEveryToolCallIsLoggedOnceWithWorkspaceToolAndOutcome(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Logged Calls", "admin@logged-calls.example.com")
	const marker = "zebra-marker-7f3a91"

	logs := captureLogs(t)
	session := e.connectTraced(mcpserver.Path, w.read, "trace-ok-000001")
	logs.Reset()
	e.ok(session, "colliedesk_whoami", nil)
	wantFields(t, "whoami", toolCallLine(t, logs, "whoami"), map[string]any{"level": "INFO", "workspace": w.id, "tool": "colliedesk_whoami",
		"token": "agent_token", "outcome": "ok", "error_type": "", "request_id": "trace-ok-000001"})

	logs.Reset()
	e.ok(session, "colliedesk_inbox_search", map[string]any{"q": marker})
	wantFields(t, "search", toolCallLine(t, logs, "search"), map[string]any{"level": "INFO", "tool": "colliedesk_inbox_search", "outcome": "ok"})

	logs.Reset()
	e.refused(session, "colliedesk_saved_reply_create", map[string]any{"title": marker, "body": marker})
	wantFields(t, "without permission", toolCallLine(t, logs, "without permission"), map[string]any{"level": "INFO", "workspace": w.id,
		"tool": "colliedesk_saved_reply_create", "token": "agent_token", "outcome": "refused", "error_type": "permission_missing"})

	logs.Reset()
	e.fails(session, "colliedesk_conversation_get", map[string]any{"conversation_id": "00000000-0000-4000-8000-000000000000"})
	wantFields(t, "unknown conversation", toolCallLine(t, logs, "unknown conversation"), map[string]any{"level": "INFO",
		"tool": "colliedesk_conversation_get", "outcome": "refused", "error_type": "not_found"})

	logs.Reset()
	e.fails(session, "colliedesk_conversation_get", map[string]any{"conversation_id": marker})
	wantFields(t, "bad argument", toolCallLine(t, logs, "bad argument"), map[string]any{"level": "INFO", "workspace": w.id,
		"tool": "colliedesk_conversation_get", "outcome": "refused", "error_type": "invalid_arguments"})

	if strings.Contains(logs.String(), marker) {
		t.Errorf("an argument reached the log:\n%s", logs.String())
	}
}

// A marker in the arguments of any call, kept in one buffer across ok and refused calls, never shows in the log.
func TestToolCallLogNeverCarriesArguments(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("Private Calls", "admin@private-calls.example.com")
	const marker = "okapi-marker-51c2d8"
	logs := captureLogs(t)
	session := e.connectTraced(mcpserver.Path, w.read, "trace-private-01")
	e.ok(session, "colliedesk_inbox_search", map[string]any{"q": marker})
	e.refused(session, "colliedesk_saved_reply_create", map[string]any{"title": marker, "body": marker})
	e.fails(session, "colliedesk_conversation_get", map[string]any{"conversation_id": marker})
	if got := len(logLines(t, logs, "mcp tool call")); got != 3 {
		t.Fatalf("%d tool call lines, want 3:\n%s", got, logs.String())
	}
	if strings.Contains(logs.String(), marker) || strings.Contains(logs.String(), w.read) {
		t.Errorf("an argument or the token reached the log:\n%s", logs.String())
	}
}

// An AI app's OAuth grant is told apart from an agent token a person made.
func TestToolCallLogNamesAnOAuthGrant(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("OAuth Calls", "admin@oauth-calls.example.com")
	access := e.connectWithOAuth(w.admin)
	logs := captureLogs(t)
	session := e.connectTraced(mcpserver.Path, access, "trace-oauth-0001")
	logs.Reset()
	e.ok(session, "colliedesk_whoami", nil)
	wantFields(t, "oauth whoami", toolCallLine(t, logs, "oauth whoami"), map[string]any{"level": "INFO", "workspace": w.id,
		"token": "oauth", "outcome": "ok", "request_id": "trace-oauth-0001"})
}

// An unplanned failure is one ERROR line, the tool call line itself: no second line for the same failure.
func TestUnplannedToolFailureIsOneErrorLine(t *testing.T) {
	e := newEnvBuilt(t, nil, func(real http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path == "/api/v1/auth/me" {
				http.Error(response, "boom", http.StatusInternalServerError)
				return
			}
			real.ServeHTTP(response, request)
		})
	})
	w := e.newWorkspace("Broken Calls", "admin@broken-calls.example.com")
	logs := captureLogs(t)
	session := e.connectTraced(mcpserver.Path, w.read, "trace-broken-001")
	logs.Reset()
	e.fails(session, "colliedesk_whoami", nil)
	wantFields(t, "internal failure", toolCallLine(t, logs, "internal failure"), map[string]any{"level": "ERROR", "workspace": w.id,
		"tool": "colliedesk_whoami", "outcome": "error", "error_type": "internal"})
	if errors := strings.Count(logs.String(), `"level":"ERROR"`); errors != 1 {
		t.Errorf("%d ERROR lines for one failure, want 1:\n%s", errors, logs.String())
	}
}

// An operator call filed for a person to approve is logged as agent_request, with the operator token kind.
func TestOperatorCallFiledForApprovalIsLoggedAsAgentRequest(t *testing.T) {
	e := newEnv(t)
	operator := operatorToken(t, e)
	logs := captureLogs(t)
	session := e.connectTraced(mcpserver.OperatorPath, operator, "trace-operator-1")
	logs.Reset()
	e.ok(session, "colliedesk_operator_token_create", map[string]any{"label": "another client", "note": "for a second AI host"})
	wantFields(t, "operator request", toolCallLine(t, logs, "operator request"), map[string]any{"level": "INFO", "workspace": "",
		"tool": "colliedesk_operator_token_create", "token": "operator", "outcome": "agent_request", "error_type": "", "request_id": "trace-operator-1"})
}
