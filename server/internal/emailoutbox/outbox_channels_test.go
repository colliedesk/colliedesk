package emailoutbox_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	channelemail "github.com/colliedesk/colliedesk/server/internal/channels/email"
	"github.com/colliedesk/colliedesk/server/internal/channels/pipeline"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// addEmailChannel adds a second support mailbox to the fixture's workspace and opens a conversation on it.
func (f fixture) addEmailChannel(t *testing.T, name, address, senderName string) (channels.Channel, string) {
	t.Helper()
	ctx := context.Background()
	config, _ := json.Marshal(map[string]string{"support_address": address, "sender_name": senderName})
	channel, err := f.store.CreateChannel(ctx, f.workspaceID, channelemail.Type, store.ChannelInput{Name: &name, Config: config}, store.ChannelActor{})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := email.Parse([]byte("From: Pat Payer <pat@customer.example>\r\nTo: "+address+"\r\n"+
		"Subject: Invoice\r\nMessage-ID: <invoice-1@customer.example>\r\n\r\nWrong amount.\r\n"), "")
	if err != nil {
		t.Fatal(err)
	}
	parsed.Platform = true
	results, err := pipeline.New(f.store.WithObjects(localStorage(t)), nil).Receive(ctx, channel, []channels.Raw{{Parsed: parsed}})
	if err != nil || results[0].ConversationID == nil {
		t.Fatalf("ingest on %s: %+v %v", name, results, err)
	}
	return channel, *results[0].ConversationID
}

// Two support mailboxes of one workspace each answer as themselves: their own sender name and
// address, and the Gmail account connected to one of them sends only its replies.
func TestEachEmailChannelSendsItsRepliesAsItself(t *testing.T) {
	f := newFixture(t)
	f.exec(t, `UPDATE email_channels SET sender_name='Outbound Support',domain_verified_at=now() WHERE workspace_id=$1`, f.workspaceID)
	billing, billingConversation := f.addEmailChannel(t, "Billing", "billing@billing.example", "Billing Team")
	fake, config := gmailStandIn(t)
	f.connectGmailTo(t, billing.ID, fake, config, "billing@billing.example")
	smtp := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := gmailWorker(f, smtp, config, &clock)

	support := f.write(t, "outgoing", "From the support desk.")
	invoice, err := f.store.CreateMessage(context.Background(), f.workspaceID, billingConversation, store.Actor{AgentID: f.agentID}, "outgoing", "From billing.")
	if err != nil {
		t.Fatal(err)
	}
	process(t, worker)

	relayed := smtp.mail()
	if len(relayed) != 1 {
		t.Fatalf("SMTP carried %d mails, want only the support reply (deliveries %+v, %+v)", len(relayed), f.delivery(t, support.ID),
			f.deliveryIn(t, billingConversation, invoice.ID))
	}
	parsed, err := mail.ReadMessage(strings.NewReader(relayed[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	if from := parsed.Header.Get("From"); from != `"Outbound Support" <support@outbound.example.com>` || !strings.Contains(relayed[0].Data, "From the support desk.") {
		t.Errorf("support reply From=%q", from)
	}
	sent := fake.Messages()
	if len(sent) != 1 {
		t.Fatalf("Gmail sent %d messages, want only the billing reply", len(sent))
	}
	gmailed, err := mail.ReadMessage(bytes.NewReader(sent[0].Raw))
	if err != nil {
		t.Fatal(err)
	}
	if from := gmailed.Header.Get("From"); from != `"Billing Team" <billing@billing.example>` || !bytes.Contains(sent[0].Raw, []byte("From billing.")) {
		t.Errorf("billing reply From=%q", from)
	}
}
