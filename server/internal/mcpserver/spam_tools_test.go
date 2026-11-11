package mcpserver_test

import (
	"strings"
	"testing"
)

// An AI agent with conversations:update files conversations in Spam and takes them out again, all or nothing,
// and the answer names the senders whose later mail the call files in Spam.
func TestConversationsSpamFilesAndUnfilesConversationsWithTheirSenders(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	args := map[string]any{"conversation_ids": []string{f.c1, f.c2}, "spam": true}

	refusedWithoutPermission(t, e, f.w.read, "colliedesk_conversations_spam", args)
	draft := e.connect(f.w.draft, "")
	out := e.ok(draft, "colliedesk_conversations_spam", args)
	if at(t, out, "updated") != float64(2) || at(t, out, "senders", 0, "name") != "Alan" || at(t, out, "senders", 1, "email") != "grace@cust.example.com" {
		t.Errorf("spam result = %v", out)
	}
	for _, id := range []string{f.c1, f.c2} {
		if after := restConversation(t, e, f.w, id); after["status"] != "spam" {
			t.Errorf("%s after spam = %v", id, after)
		}
	}
	if again := e.ok(draft, "colliedesk_conversations_spam", args); at(t, again, "updated") != float64(0) {
		t.Errorf("repeating it = %v", again)
	}

	mixed := map[string]any{"conversation_ids": []string{f.c3, f.otherConvID}, "spam": true}
	if message := e.fails(draft, "colliedesk_conversations_spam", mixed); !strings.Contains(message, "nothing was changed") {
		t.Errorf("a list with another workspace's id: %q", message)
	}
	if after := restConversation(t, e, f.w, f.c3); after["status"] != "open" {
		t.Errorf("a refused call changed %s: %v", f.c3, after)
	}
	if after := restConversation(t, e, f.other, f.otherConvID); after["status"] != "open" {
		t.Errorf("the other workspace's conversation changed: %v", after)
	}

	back := e.ok(draft, "colliedesk_conversations_spam", map[string]any{"conversation_ids": []string{f.c1}, "spam": false})
	if at(t, back, "updated") != float64(1) {
		t.Errorf("not spam result = %v", back)
	}
	if after := restConversation(t, e, f.w, f.c1); after["status"] != "pending" {
		t.Errorf("Not spam did not put back pending: %v", after)
	}
	if message := e.fails(draft, "colliedesk_conversation_update", map[string]any{"conversation_id": f.c2, "status": "open"}); !strings.Contains(message, "colliedesk_conversations_spam") {
		t.Errorf("a status change of a spam conversation: %q", message)
	}
}
