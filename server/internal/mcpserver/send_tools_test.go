package mcpserver_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// withSMTP configures outgoing email, so a reply is queued for delivery instead of failing at once.
func withSMTP(config *httpapi.Config) {
	config.Email = email.Config{SMTP: email.SMTPConfig{Host: "smtp.test.invalid", Port: 587, StartTLS: true}, FromPolicy: email.FromPolicyAny}
}

// requestedForTier calls the tool with a token one tier too low: the call is filed as an agent request
// a person approves, which the model reads as a result (not an error) that says nothing changed yet.
func requestedForTier(t *testing.T, e *env, token, tool string, args map[string]any) map[string]any {
	t.Helper()
	out := e.ok(e.connect(token, ""), tool, args)
	if id, _ := at(t, out, "agent_request", "id").(string); id == "" {
		t.Fatalf("%s below its tier: result %v, want an agent request", tool, out)
	}
	if next, _ := out["next"].(string); !strings.Contains(next, "did not do this yet") || !strings.Contains(next, "colliedesk_request_get") {
		t.Errorf("%s below its tier: next %q must say nothing happened yet and how to follow the request", tool, next)
	}
	return out
}

func messageKinds(t *testing.T, e *env, w workspace, conversationID string) []string {
	t.Helper()
	status, listed := e.rest(http.MethodGet, "/api/v1/conversations/"+conversationID+"/messages", nil, w.admin)
	if status != http.StatusOK {
		t.Fatalf("GET messages status=%d", status)
	}
	kinds := []string{}
	for _, item := range listed["items"].([]any) {
		kinds = append(kinds, at(t, item, "kind").(string))
	}
	return kinds
}

func TestReplySendEmailsTheCustomerOnlyWithASendToken(t *testing.T) {
	e := newEnvWith(t, withSMTP)
	f := seedInbox(t, e)
	if status, body := e.rest(http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "help@inbox.example.com"}, f.w.admin); status != http.StatusOK {
		t.Fatalf("connect email: %d %v", status, body)
	}
	args := map[string]any{"conversation_id": f.c2, "body": "Your refund is on its way.", "seen_reply_number": 0}

	if out := requestedForTier(t, e, f.w.draft, "colliedesk_reply_send", args); at(t, out, "agent_request", "kind") != "reply_draft" {
		t.Errorf("a draft token's reply must become a reply draft: %v", out)
	}
	if kinds := messageKinds(t, e, f.w, f.c2); len(kinds) != 0 {
		t.Fatalf("a reply waiting for approval left messages: %v", kinds)
	}
	out := e.ok(e.connect(f.w.send, ""), "colliedesk_reply_send", args)
	if at(t, out, "message", "kind") != "outgoing" || at(t, out, "message", "delivery") != "queued" || at(t, out, "message", "written_by_agent_token", "scope") != "send" {
		t.Errorf("reply result = %v, want an outgoing reply queued for email and attributed to the token", out)
	}
	if kinds := messageKinds(t, e, f.w, f.c2); !slices.Equal(kinds, []string{"outgoing"}) {
		t.Errorf("timeline kinds = %v, want one outgoing reply", kinds)
	}
	if message := e.fails(e.connect(f.w.send, ""), "colliedesk_reply_send", map[string]any{"conversation_id": f.otherConvID, "body": "hi", "seen_reply_number": 0}); !strings.Contains(message, "No conversation") {
		t.Errorf("another workspace's conversation: %q", message)
	}
	if kinds := messageKinds(t, e, f.other, f.otherConvID); len(kinds) != 0 {
		t.Errorf("the other workspace got messages: %v", kinds)
	}
}

func TestReplyRetryRequeuesAFailedDelivery(t *testing.T) {
	e := newEnvWith(t, withSMTP)
	f := seedInbox(t, e)
	e.rest(http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "help@inbox.example.com"}, f.w.admin)
	reply := e.created("/api/v1/conversations/"+f.c2+"/messages", map[string]any{"kind": "outgoing", "body": "First try"}, f.w.admin)
	if _, err := e.pool.Exec(context.Background(), `UPDATE channel_outbox SET status='failed', last_error='550 mailbox unavailable' WHERE message_id=$1`, reply); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"conversation_id": f.c2, "message_id": reply}

	requestedForTier(t, e, f.w.draft, "colliedesk_reply_retry", args)
	if message := e.fails(e.connect(f.other.send, ""), "colliedesk_reply_retry", args); !strings.Contains(message, "No reply") {
		t.Errorf("another workspace's token: %q", message)
	}
	out := e.ok(e.connect(f.w.send, ""), "colliedesk_reply_retry", args)
	if at(t, out, "delivery") != "queued" || at(t, out, "message_id") != reply {
		t.Errorf("retry result = %v, want the reply queued again", out)
	}
	if message := e.fails(e.connect(f.w.send, ""), "colliedesk_reply_retry", args); !strings.Contains(message, "failed") {
		t.Errorf("a second retry of a queued delivery must say only failed ones retry: %q", message)
	}
}

func TestConversationAndContactCreateNeedTheSendTier(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	contactArgs := map[string]any{"name": "Linus", "email": "linus@cust.example.com"}

	requestedForTier(t, e, f.w.draft, "colliedesk_contact_create", contactArgs)
	send := e.connect(f.w.send, "")
	contact := e.ok(send, "colliedesk_contact_create", contactArgs)
	contactID := at(t, contact, "contact", "id").(string)
	if at(t, contact, "contact", "untrusted_customer_content", "email") != "linus@cust.example.com" {
		t.Errorf("contact = %v", contact)
	}

	conversationArgs := map[string]any{"contact_id": contactID, "subject": "Welcome aboard"}
	requestedForTier(t, e, f.w.draft, "colliedesk_conversation_create", conversationArgs)
	out := e.ok(send, "colliedesk_conversation_create", conversationArgs)
	created := restConversation(t, e, f.w, at(t, out, "conversation", "id").(string))
	if created["subject"] != "Welcome aboard" || created["contact_id"] != contactID || at(t, out, "conversation", "status") != "open" {
		t.Errorf("created conversation = %v (tool said %v)", created, out)
	}

	_, other := e.rest(http.MethodGet, "/api/v1/conversations/"+f.otherConvID, nil, f.other.admin)
	if message := e.fails(send, "colliedesk_conversation_create", map[string]any{"contact_id": other["contact_id"], "subject": "Steal"}); !strings.Contains(message, "No contact") {
		t.Errorf("a conversation for another workspace's contact: %q", message)
	}
	status, listed := e.rest(http.MethodGet, "/api/v1/contacts?search=linus", nil, f.other.admin)
	if status != http.StatusOK || len(listed["items"].([]any)) != 0 {
		t.Errorf("the contact leaked into the other workspace: %v", listed)
	}
}

func TestTagRenameAndDeleteNeedTheSendTierAndStayInTheWorkspace(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	e.created("/api/v1/tags", map[string]any{"name": "billing"}, f.other.admin)

	requestedForTier(t, e, f.w.draft, "colliedesk_tag_rename", map[string]any{"tag": "billing", "new_name": "invoices"})
	send := e.connect(f.w.send, "")
	out := e.ok(send, "colliedesk_tag_rename", map[string]any{"tag": "billing", "new_name": "invoices"})
	if at(t, out, "tag", "name") != "invoices" {
		t.Errorf("rename result = %v", out)
	}
	if got := tagNames(t, restConversation(t, e, f.w, f.c1)); !slices.Equal(got, []string{"invoices"}) {
		t.Errorf("conversation tags after rename = %v", got)
	}
	if got := workspaceTagNames(t, e, f.other.admin); !slices.Equal(got, []string{"billing"}) {
		t.Errorf("the other workspace's tag changed: %v", got)
	}

	requestedForTier(t, e, f.w.draft, "colliedesk_tag_delete", map[string]any{"tag": "invoices"})
	if message := e.fails(send, "colliedesk_tag_delete", map[string]any{"tag": "billing"}); !strings.Contains(message, "No tag named") {
		t.Errorf("deleting a name only the other workspace has: %q", message)
	}
	deleted := e.ok(send, "colliedesk_tag_delete", map[string]any{"tag": "invoices"})
	if at(t, deleted, "deleted", "name") != "invoices" {
		t.Errorf("delete result = %v", deleted)
	}
	if got := workspaceTagNames(t, e, f.w.admin); len(got) != 0 {
		t.Errorf("tags left = %v", got)
	}
	if got := tagNames(t, restConversation(t, e, f.w, f.c1)); len(got) != 0 {
		t.Errorf("the deleted tag is still on the conversation: %v", got)
	}
	if got := workspaceTagNames(t, e, f.other.admin); !slices.Equal(got, []string{"billing"}) {
		t.Errorf("the other workspace lost its tag: %v", got)
	}
}

func TestConversationsBulkUpdateChangesManyAtOnceAndAllOrNothing(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	args := map[string]any{"conversation_ids": []string{f.c2, f.c3}, "status": "resolved", "assignee": "me", "add_tags": []string{"billing"}}

	requestedForTier(t, e, f.w.read, "colliedesk_conversations_bulk_update", args)
	draft := e.connect(f.w.draft, "")
	out := e.ok(draft, "colliedesk_conversations_bulk_update", args)
	if at(t, out, "updated") != float64(2) {
		t.Errorf("bulk result = %v", out)
	}
	for _, id := range []string{f.c2, f.c3} {
		after := restConversation(t, e, f.w, id)
		if after["status"] != "resolved" || after["assignee_id"] != f.adminID || !slices.Equal(tagNames(t, after), []string{"billing"}) {
			t.Errorf("%s after bulk = %v", id, after)
		}
	}
	e.ok(draft, "colliedesk_conversations_bulk_update", args)

	mixed := map[string]any{"conversation_ids": []string{f.c1, f.otherConvID}, "status": "closed"}
	if message := e.fails(draft, "colliedesk_conversations_bulk_update", mixed); !strings.Contains(message, "nothing was changed") {
		t.Errorf("a list with another workspace's id: %q", message)
	}
	if after := restConversation(t, e, f.w, f.c1); after["status"] != "pending" {
		t.Errorf("a refused bulk update changed %s: %v", f.c1, after)
	}
	if after := restConversation(t, e, f.other, f.otherConvID); after["status"] != "open" {
		t.Errorf("the other workspace's conversation changed: %v", after)
	}
	if message := e.fails(draft, "colliedesk_conversations_bulk_update", map[string]any{"conversation_ids": []string{f.c1}, "add_tags": []string{"nonesuch"}}); !strings.Contains(message, "No tag named") {
		t.Errorf("an unknown tag: %q", message)
	}
}

func TestTokenRevokeEndsTheTokenItWasCalledWith(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	session := e.connect(f.w.read, "")

	// The result is read without listing the tools again: the token that would list them is gone.
	result := e.call(session, "colliedesk_token_revoke", nil)
	if result.IsError || at(t, structured(t, result), "revoked") != true {
		t.Errorf("revoke result = %s", text(result))
	}
	if status, _ := e.rest(http.MethodGet, "/api/v1/auth/me", nil, f.w.read); status != http.StatusUnauthorized {
		t.Errorf("the revoked token still works: %d", status)
	}
	for _, token := range []string{f.w.draft, f.w.admin, f.other.read} {
		if status, _ := e.rest(http.MethodGet, "/api/v1/auth/me", nil, token); status != http.StatusOK {
			t.Errorf("revoking one token ended another: %d", status)
		}
	}
}
