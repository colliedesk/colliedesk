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

// Mail moved into a conversation from another customer's is not that conversation's thread: a reply
// goes to the conversation's customer only, threaded on their own mail, never naming the moved mail.
func TestAReplyIsNeverThreadedOnMailMovedInFromAnotherCustomer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	parsed, err := email.Parse([]byte("From: Bob Other <bob@other.example>\r\nTo: support@outbound.example.com\r\n"+
		"Subject: Export broken too\r\nMessage-ID: <bob-1@other.example>\r\n\r\nSame here.\r\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	parsed.Platform = true
	bob, err := ingestEmail(t, ctx, f.store, f.workspaceID, parsed)
	if err != nil || bob.ConversationID == nil {
		t.Fatalf("ingest: %+v %v", bob, err)
	}
	moved, err := f.store.MoveMessages(ctx, f.workspaceID, *bob.ConversationID, store.MoveInput{MessageIDs: []string{*bob.MessageID},
		To: store.MoveTarget{ConversationID: f.conversationID}}, store.Actor{AgentID: f.agentID}, time.Now().UTC())
	if err != nil || len(moved.Warnings) != 1 || moved.Warnings[0] != store.MoveWarningOtherContact {
		t.Fatalf("move = %+v, %v", moved, err)
	}
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	f.write(t, "outgoing", "We are on it.")
	if process(t, newWorker(f, server, &clock)) != 1 {
		t.Fatal("reply not sent")
	}
	received := server.mail()
	if len(received) != 1 || len(received[0].To) != 1 || received[0].To[0] != "olive@customer.example" {
		t.Fatalf("envelope = %+v", received)
	}
	parsedReply, err := mail.ReadMessage(strings.NewReader(received[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	if got := parsedReply.Header.Get("In-Reply-To"); got != "<export-1@customer.example>" {
		t.Fatalf("In-Reply-To = %q", got)
	}
	if got := parsedReply.Header.Get("References"); strings.Contains(got, "bob-1") || strings.Contains(received[0].Data, "bob@other.example") {
		t.Fatalf("the reply names the moved mail: References %q", got)
	}
}
