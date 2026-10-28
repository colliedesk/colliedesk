package emailoutbox_test

import (
	"context"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A conversation the team starts is a new email thread: the subject as written, no reply headers, and
// a Message-ID and reply address that bring the customer's answer back into the same conversation.
func TestNewConversationEmailStartsAThreadTheCustomerReplyJoins(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	contact, err := f.store.CreateContact(ctx, f.workspaceID, "Pat Prospect", "pat@prospect.example")
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := f.store.StartConversation(ctx, f.workspaceID, store.NewConversation{ContactID: contact.ID, Subject: "Your renewal"},
		store.Actor{AgentID: f.agentID}, "Your plan renews next week.", nil)
	if err != nil {
		t.Fatal(err)
	}
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	if process(t, newWorker(f, server, &clock)) != 1 {
		t.Fatal("the opening email was not sent")
	}
	received := server.mail()
	if len(received) != 1 || received[0].To[0] != "pat@prospect.example" {
		t.Fatalf("unexpected envelope: %+v", received)
	}
	parsed, err := mail.ReadMessage(strings.NewReader(received[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Header.Get("Subject"); got != "Your renewal" {
		t.Errorf("Subject=%q, want the subject as written", got)
	}
	for _, name := range []string{"In-Reply-To", "References", "Auto-Submitted"} {
		if got := parsed.Header.Get(name); got != "" {
			t.Errorf("%s=%q on the first email of a thread, want none", name, got)
		}
	}
	sent := strings.Trim(parsed.Header.Get("Message-Id"), "<>")
	if sent == "" {
		t.Fatal("the opening email has no Message-ID")
	}

	answer, err := email.Parse([]byte("From: Pat Prospect <pat@prospect.example>\r\nTo: support@outbound.example.com\r\n"+
		"Subject: Re: Your renewal\r\nMessage-ID: <renewal-answer@prospect.example>\r\nIn-Reply-To: <"+sent+">\r\n\r\nThanks, renew it.\r\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	answer.Platform = true
	result, err := ingestEmail(t, ctx, f.store, f.workspaceID, answer)
	if err != nil || result.ConversationID == nil || *result.ConversationID != conversation.ID {
		t.Fatalf("customer answer = %+v, %v; want it threaded into %s", result, err, conversation.ID)
	}

	// A later reply in the same conversation answers the customer's message, with "Re:".
	if _, err := f.store.CreateMessage(ctx, f.workspaceID, conversation.ID, store.Actor{AgentID: f.agentID}, "outgoing", "Done."); err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	if process(t, newWorker(f, server, &clock)) != 1 {
		t.Fatal("the follow-up was not sent")
	}
	followUp, err := mail.ReadMessage(strings.NewReader(server.mail()[1].Data))
	if err != nil {
		t.Fatal(err)
	}
	if got := followUp.Header.Get("Subject"); got != "Re: Your renewal" {
		t.Errorf("follow-up Subject=%q, want Re: Your renewal", got)
	}
	if got := followUp.Header.Get("In-Reply-To"); got != "<renewal-answer@prospect.example>" {
		t.Errorf("follow-up In-Reply-To=%q, want the customer's message", got)
	}
}
