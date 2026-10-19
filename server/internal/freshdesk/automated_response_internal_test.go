package freshdesk

import (
	"encoding/json"
	"testing"
	"time"
)

// Freshdesk marks an auto-reply (an out-of-office, a chat-bot or messenger acknowledgement) with
// auto_response on the conversation. The importer keeps that as the origin kind, so the timeline can fold it.
func TestConversationMarkedAutoResponseKeepsThatInItsOrigin(t *testing.T) {
	r := &run{}
	var auto fdConversation
	payload := `{"id": 7101, "body": "<div>We received your message</div>", "body_text": "We received your message", "incoming": true, "private": false,
		"user_id": 5, "source": 0, "category": 1, "from_email": "bot@sample.example", "to_emails": ["support@acme.example"], "auto_response": true,
		"created_at": "2026-08-11T02:20:30Z"}`
	if err := json.Unmarshal([]byte(payload), &auto); err != nil {
		t.Fatal(err)
	}
	if !auto.AutoResponse {
		t.Fatal("auto_response was not read from the conversation")
	}
	got := r.conversationMessage(auto)
	if got.kind != "incoming" || got.origin.Kind != "automated_response" || got.origin.From == nil || *got.origin.From != "bot@sample.example" {
		t.Fatalf("kind=%s origin=%+v", got.kind, got.origin)
	}

	plain := auto
	plain.AutoResponse = false
	if got := r.conversationMessage(plain); got.origin.Kind != "incoming" {
		t.Errorf("an ordinary reply from the customer became %q", got.origin.Kind)
	}
	// An agent's own note stays a note even when Freshdesk flags it; only customer-side mail is an auto-reply.
	note := auto
	note.Private, note.Incoming = true, false
	if got := r.conversationMessage(note); got.kind != "note" || got.origin.Kind != "private_note" {
		t.Errorf("a flagged private note became kind=%s origin=%s", got.kind, got.origin.Kind)
	}
	_ = time.Now
}
