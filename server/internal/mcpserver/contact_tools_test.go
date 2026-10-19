package mcpserver_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestContactUpdateRenamesAndNeedsTheSendTier(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	_, conversation := e.rest(http.MethodGet, "/api/v1/conversations/"+f.c1, nil, f.w.admin)
	grace := conversation["contact_id"].(string)
	args := map[string]any{"contact_id": grace, "name": "Grace Hopper"}

	requestedForTier(t, e, f.w.draft, "colliedesk_contact_update", args)
	out := e.ok(e.connect(f.w.send, ""), "colliedesk_contact_update", args)
	if at(t, out, "contact", "untrusted_customer_content", "name") != "Grace Hopper" {
		t.Fatalf("rename = %v", out)
	}
	_, other := e.rest(http.MethodGet, "/api/v1/conversations/"+f.otherConvID, nil, f.other.admin)
	if message := e.fails(e.connect(f.w.send, ""), "colliedesk_contact_update", map[string]any{"contact_id": other["contact_id"], "name": "Taken"}); !strings.Contains(message, "No contact") {
		t.Errorf("renaming another workspace's contact: %q", message)
	}
}

func TestContactMergeMovesConversationsAndUnmergeMovesThemBack(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	_, first := e.rest(http.MethodGet, "/api/v1/conversations/"+f.c1, nil, f.w.admin)
	_, second := e.rest(http.MethodGet, "/api/v1/conversations/"+f.c2, nil, f.w.admin)
	grace, alan := first["contact_id"].(string), second["contact_id"].(string)
	args := map[string]any{"contact_id": grace, "duplicate_contact_id": alan}

	requestedForTier(t, e, f.w.draft, "colliedesk_contact_merge", args)
	send := e.connect(f.w.send, "")
	merged := e.ok(send, "colliedesk_contact_merge", args)
	mergeID, _ := at(t, merged, "merge", "merge_id").(string)
	if mergeID == "" || at(t, merged, "merge", "conversations_moved") != float64(1) ||
		at(t, merged, "merge", "merged_contact", "untrusted_customer_content", "email") != "alan@cust.example.com" {
		t.Fatalf("merge = %v", merged)
	}
	read := e.connect(f.w.read, "")
	kept := e.ok(read, "colliedesk_contact_get", map[string]any{"contact_id": grace, "limit": 2})
	if at(t, kept, "conversations_total") != float64(3) || len(kept["conversations"].([]any)) != 2 || at(t, kept, "merges", 0, "merge_id") != mergeID {
		t.Fatalf("kept contact = %v, want 3 conversations (2 shown) and the merge", kept)
	}
	if gone := e.ok(read, "colliedesk_contact_get", map[string]any{"contact_id": alan}); gone["merged_into_id"] != grace {
		t.Fatalf("merged contact = %v, want merged_into_id %s", gone, grace)
	}

	undone := e.ok(send, "colliedesk_contact_unmerge", map[string]any{"contact_id": grace, "merge_id": mergeID})
	if at(t, undone, "merge", "undone_at") == nil {
		t.Fatalf("unmerge = %v", undone)
	}
	if back := e.ok(read, "colliedesk_contact_get", map[string]any{"contact_id": alan}); back["merged_into_id"] != nil || at(t, back, "conversations_total") != float64(1) {
		t.Fatalf("after unmerge = %v", back)
	}
	if message := e.fails(send, "colliedesk_contact_unmerge", map[string]any{"contact_id": grace, "merge_id": mergeID}); !strings.Contains(message, "undone") {
		t.Errorf("a second unmerge: %q", message)
	}
	_, other := e.rest(http.MethodGet, "/api/v1/conversations/"+f.otherConvID, nil, f.other.admin)
	if message := e.fails(send, "colliedesk_contact_merge", map[string]any{"contact_id": grace, "duplicate_contact_id": other["contact_id"]}); !strings.Contains(message, "No contact") {
		t.Errorf("merging another workspace's contact: %q", message)
	}
}

func TestConversationStartEmailsTheCustomerInOneStep(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	_, conversation := e.rest(http.MethodGet, "/api/v1/conversations/"+f.c1, nil, f.w.admin)
	grace := conversation["contact_id"].(string)
	args := map[string]any{"contact_id": grace, "subject": "Your renewal", "body": "It renews next week."}

	send := e.connect(f.w.send, "")
	if message := e.fails(send, "colliedesk_conversation_start", args); !strings.Contains(message, "email channel") {
		t.Fatalf("without an email channel: %q", message)
	}
	if status, body := e.rest(http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "support@inbox.example.com"}, f.w.admin); status != http.StatusOK {
		t.Fatalf("connect email: %d %v", status, body)
	}
	filed := requestedForTier(t, e, f.w.draft, "colliedesk_conversation_start", args)
	if summary, _ := at(t, filed, "agent_request", "summary").(string); !strings.Contains(summary, "grace@cust.example.com") || !strings.Contains(summary, "It renews next week.") {
		t.Errorf("the approver must see who is emailed and what: summary %q", summary)
	}
	out := e.ok(send, "colliedesk_conversation_start", args)
	id := at(t, out, "conversation", "id").(string)
	if at(t, out, "message", "delivery") == nil {
		t.Errorf("start = %v, want the first reply's delivery", out)
	}
	if kinds := messageKinds(t, e, f.w, id); strings.Join(kinds, ",") != "outgoing" {
		t.Errorf("timeline kinds = %v, want one outgoing reply", kinds)
	}
	added := e.ok(send, "colliedesk_conversation_start", map[string]any{"new_contact": map[string]any{"name": "Nia New", "email": "nia@new.example"},
		"subject": "Welcome", "body": "Hello Nia."})
	if _, ok := at(t, added, "conversation", "id").(string); !ok {
		t.Fatalf("start with a new customer = %v", added)
	}
	if message := e.fails(send, "colliedesk_conversation_start", map[string]any{"new_contact": map[string]any{"name": "Grace", "email": "grace@cust.example.com"},
		"subject": "Again", "body": "Hi."}); !strings.Contains(message, "already a customer") {
		t.Errorf("an existing address as a new customer: %q", message)
	}
}
