package mcpserver_test

import (
	"net/http"
	"strings"
	"testing"
)

// An AI agent files a request for an article from a conversation in one call; the request lands in the
// knowledge gaps with the conversation it came from, a close wording joins it, and one more call takes
// it back.
func TestAnAgentRequestsAnArticleFromAConversationInOneCall(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	read := e.connect(f.w.read, "")

	filed := e.ok(read, "colliedesk_kb_article_ask", map[string]any{"conversation_id": f.c2, "title": "Refund a payment", "note": "Asked twice"})
	e.requireMatchesOutputSchema(read, "colliedesk_kb_article_ask", filed)
	gap, _ := at(t, filed, "gap", "id").(string)
	if filed["created"] != true || filed["added_to_existing"] != false || at(t, filed, "gap", "conversation_count") != float64(1) ||
		at(t, filed, "gap", "untrusted_customer_content", "title") != "Refund a payment" {
		t.Fatalf("request = %v, want a new gap titled Refund a payment from one conversation", filed)
	}
	joined := e.ok(read, "colliedesk_kb_article_ask", map[string]any{"conversation_id": f.c1, "title": "How to refund a payment"})
	if at(t, joined, "gap", "id") != gap || joined["added_to_existing"] != true || at(t, joined, "gap", "conversation_count") != float64(2) {
		t.Fatalf("joined = %v, want gap %s from two conversations", joined, gap)
	}
	similar := e.ok(read, "colliedesk_kb_gaps_list", map[string]any{"similar": "refund my payment"})["items"].([]any)
	if len(similar) != 1 || at(t, similar[0], "id") != gap {
		t.Fatalf("similar = %v, want gap %s", similar, gap)
	}
	detail := e.ok(read, "colliedesk_kb_gap_get", map[string]any{"gap_id": gap})
	e.requireMatchesOutputSchema(read, "colliedesk_kb_gap_get", detail)
	conversations := detail["conversations"].([]any)
	if len(conversations) != 2 || at(t, conversations[0], "conversation_id") != f.c1 || at(t, conversations[0], "display_ref") == "" {
		t.Fatalf("conversations = %v, want the two that asked, newest first", conversations)
	}

	withdrawn := e.ok(read, "colliedesk_kb_article_ask_withdraw", map[string]any{"conversation_id": f.c1})
	if withdrawn["withdrawn"] != true {
		t.Fatalf("withdraw = %v", withdrawn)
	}
	if message := e.fails(read, "colliedesk_kb_article_ask", map[string]any{"conversation_id": f.otherConvID}); !strings.Contains(message, "not found") {
		t.Fatalf("another workspace's conversation: %q, want not found", message)
	}

	// The writer starts a draft from the request: the editor opens beside the conversation that asked.
	send := e.connect(f.w.send, "")
	e.ok(send, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"})
	draft := e.ok(send, "colliedesk_kb_gap_article_start", map[string]any{"gap_id": gap})
	e.requireMatchesOutputSchema(send, "colliedesk_kb_gap_article_start", draft)
	if at(t, draft, "article", "status") != "draft" || at(t, draft, "gap_source", "requested") != true ||
		at(t, draft, "gap_source", "conversations", 0, "conversation_id") != f.c2 {
		t.Fatalf("draft = %v, want a draft beside conversation %s", draft, f.c2)
	}
}

// An AI agent makes a draft article from a reply the team sent, in one call, with the customer's
// details masked.
func TestAnAgentMakesAnArticleFromASentReplyInOneCall(t *testing.T) {
	e := newEnv(t)
	f := seedInbox(t, e)
	e.incoming(f.w, f.c2, "Grace here, refund order 44556677 to grace@cust.example.com please")
	_, reply := e.rest(http.MethodPost, "/api/v1/conversations/"+f.c2+"/messages", map[string]any{"kind": "outgoing",
		"body": "Open Billing, pick the order and press Refund. Call 0912345678 if it fails."}, f.w.admin)
	send := e.connect(f.w.send, "")
	e.ok(send, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"})
	made := e.ok(send, "colliedesk_kb_article_from_reply", map[string]any{"conversation_id": f.c2, "message_id": reply["id"]})
	e.requireMatchesOutputSchema(send, "colliedesk_kb_article_from_reply", made)
	body, _ := at(t, made, "article", "team_text", "body").(string)
	question, _ := at(t, made, "reply_source", "untrusted_customer_content", "question").(string)
	if at(t, made, "article", "status") != "draft" || !strings.Contains(body, "press Refund") || strings.Contains(body, "0912345678") ||
		!strings.Contains(question, "refund order") || strings.Contains(question, "44556677") || strings.Contains(question, "grace@") {
		t.Fatalf("made = %v, want a masked draft holding the reply and the question", made)
	}
	e.refused(e.connect(f.w.read, ""), "colliedesk_kb_article_from_reply", map[string]any{"conversation_id": f.c2, "message_id": reply["id"]})
}
