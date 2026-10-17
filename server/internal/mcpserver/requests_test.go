package mcpserver_test

import (
	"net/http"
	"strings"
	"testing"
)

func restRequest(t *testing.T, e *env, token, id string) map[string]any {
	t.Helper()
	status, body := e.rest(http.MethodGet, "/api/v1/agent-requests/"+id, nil, token)
	if status != http.StatusOK {
		t.Fatalf("GET agent request %s: %d %v", id, status, body)
	}
	return body
}

func TestReplyDraftToolLeavesADraftThatOnlyAPersonSends(t *testing.T) {
	e := newEnvWith(t, withSMTP)
	f := seedInbox(t, e)
	// Even a send token only drafts with this tool.
	out := e.ok(e.connect(f.w.send, ""), "colliedesk_reply_draft", map[string]any{"conversation_id": f.c2, "body": "Your refund is approved."})
	id, _ := at(t, out, "agent_request", "id").(string)
	if id == "" || at(t, out, "agent_request", "kind") != "reply_draft" || at(t, out, "agent_request", "status") != "pending" {
		t.Fatalf("reply_draft result = %v", out)
	}
	if kinds := messageKinds(t, e, f.w, f.c2); len(kinds) != 0 {
		t.Fatalf("a draft reached the timeline: %v", kinds)
	}
	if message := e.fails(e.connect(f.w.send, ""), "colliedesk_reply_draft", map[string]any{"conversation_id": f.otherConvID, "body": "hi"}); !strings.Contains(message, "No conversation") {
		t.Errorf("draft on another workspace's conversation: %q", message)
	}

	// The model follows its request; approving is a person's call and no tool does it.
	session := e.connect(f.w.send, "")
	listed := e.ok(session, "colliedesk_requests_list", map[string]any{"status": "pending"})
	requests, _ := listed["requests"].([]any)
	if len(requests) != 1 || at(t, requests[0], "id") != id || at(t, requests[0], "call") != "POST /api/v1/conversations/"+f.c2+"/messages" {
		t.Fatalf("requests_list = %v", listed)
	}
	if status, body := e.rest(http.MethodPost, "/api/v1/agent-requests/"+id+"/approve", nil, f.w.send); status != http.StatusForbidden {
		t.Fatalf("a token approved its own request: %d %v", status, body)
	}
	if status, body := e.rest(http.MethodPost, "/api/v1/agent-requests/"+id+"/approve", map[string]any{"body": "Your refund is approved. It arrives in 3 days."}, f.w.admin); status != http.StatusOK {
		t.Fatalf("person approves: %d %v", status, body)
	}
	got := e.ok(session, "colliedesk_request_get", map[string]any{"request_id": id})
	if at(t, got, "request", "status") != "approved" || at(t, got, "request", "decided_by") != "Ada Admin" || at(t, got, "request", "result_status") != float64(201) {
		t.Errorf("request after approval = %v", got)
	}
	if kinds := messageKinds(t, e, f.w, f.c2); len(kinds) != 1 || kinds[0] != "outgoing" {
		t.Errorf("timeline after Send = %v, want the one reply", kinds)
	}
}

func TestRequestCancelWithdrawsOnlyTheTokensOwnRequest(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	out := requestedForTier(t, e, f.w.read, "colliedesk_note_add", map[string]any{"conversation_id": f.c1, "body": "Waiting on the vendor."})
	id := at(t, out, "agent_request", "id").(string)

	// Another token of the workspace and a token of another workspace see nothing.
	for _, token := range []string{f.w.draft, f.other.send} {
		session := e.connect(token, "")
		if listed := e.ok(session, "colliedesk_requests_list", nil); len(listed["requests"].([]any)) != 0 {
			t.Errorf("another token lists %v", listed)
		}
		if message := e.fails(session, "colliedesk_request_cancel", map[string]any{"request_id": id}); !strings.Contains(message, "No request") {
			t.Errorf("another token cancels: %q", message)
		}
	}
	session := e.connect(f.w.read, "")
	cancelled := e.ok(session, "colliedesk_request_cancel", map[string]any{"request_id": id})
	if at(t, cancelled, "request", "status") != "cancelled" {
		t.Fatalf("cancel = %v", cancelled)
	}
	if message := e.fails(session, "colliedesk_request_cancel", map[string]any{"request_id": id}); !strings.Contains(message, "already") {
		t.Errorf("second cancel: %q", message)
	}
	if status, _ := e.rest(http.MethodPost, "/api/v1/agent-requests/"+id+"/approve", nil, f.w.admin); status != http.StatusConflict {
		t.Errorf("approve after cancel: %d, want 409", status)
	}
	if kinds := messageKinds(t, e, f.w, f.c1); len(kinds) != 0 {
		t.Errorf("a cancelled note was written: %v", kinds)
	}
}

func TestApprovalToolsFileARequestForEveryTier(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	calls := []struct {
		tool string
		args map[string]any
		call string
	}{
		{"colliedesk_token_create_request", map[string]any{"label": "Nightly triage", "scope": "draft"}, "POST /api/v1/auth/tokens"},
		{"colliedesk_workspace_export_request", nil, "POST /api/v1/export/link"},
		{"colliedesk_workspace_export_request", map[string]any{"delivery": "download"}, "GET /api/v1/export"},
		{"colliedesk_email_channel_disconnect_request", nil, "DELETE /api/v1/channels/email"},
		{"colliedesk_email_secret_rotate_request", nil, "POST /api/v1/channels/email/secret"},
		{"colliedesk_workspace_slug_change_request", map[string]any{"slug": "renamed-desk"}, "PATCH /api/v1/workspace/slug"},
	}
	for _, token := range []string{f.w.read, f.w.send} {
		session := e.connect(token, "")
		for _, c := range calls {
			out := e.ok(session, c.tool, c.args)
			id, _ := at(t, out, "agent_request", "id").(string)
			if id == "" {
				t.Errorf("%s: %v, want an agent request", c.tool, out)
				continue
			}
			got := e.ok(session, "colliedesk_request_get", map[string]any{"request_id": id})
			if at(t, got, "request", "call") != c.call || at(t, got, "request", "status") != "pending" {
				t.Errorf("%s filed %v, want a pending %s", c.tool, got, c.call)
			}
		}
	}
	_, me := e.rest(http.MethodGet, "/api/v1/auth/me", nil, f.w.admin)
	if at(t, me, "workspace", "slug") == "renamed-desk" {
		t.Fatal("the slug changed without a person's approval")
	}
}
