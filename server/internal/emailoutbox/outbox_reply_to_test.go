package emailoutbox_test

import (
	"context"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// waitingDesk is a workspace whose email channel is connected but has received nothing yet: the team
// writes first, as a team does while its mailbox still forwards to the old help desk.
func waitingDesk(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	pool := testPool(t)
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	workspace, _, err := auth.New(pool, "https://desk.example.com").CreateWorkspace(ctx, "Waiting Desk", "admin@waiting.example.com")
	if err != nil {
		t.Fatal(err)
	}
	st := channelStore(t, pool).WithOutbound(store.OutboundPolicy{Enabled: true})
	channel, err := st.UpsertEmailChannel(ctx, workspace.ID, "support@waiting.example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	var agentID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE workspace_id=$1`, workspace.ID).Scan(&agentID); err != nil {
		t.Fatal(err)
	}
	return fixture{store: st, pool: pool, workspaceID: workspace.ID, emailChannelID: channel.ChannelID, agentID: agentID}
}

// arrive files one inbound email on the desk's email channel. direct marks mail that did not pass the
// support mailbox (a test message, an answer to a platform reply address).
func (f fixture) arrive(t *testing.T, raw string, platform, direct bool) {
	t.Helper()
	parsed, err := email.Parse([]byte(raw), "")
	if err != nil {
		t.Fatal(err)
	}
	parsed.Platform, parsed.Direct = platform, direct
	if _, err := ingestEmail(t, context.Background(), f.store, f.workspaceID, parsed); err != nil {
		t.Fatalf("ingest: %v", err)
	}
}

// supportAddress saves the desk's support address, as Settings → Email does.
func (f fixture) supportAddress(t *testing.T, address string) {
	t.Helper()
	if _, err := f.store.UpsertEmailChannel(context.Background(), f.workspaceID, address, nil); err != nil {
		t.Fatal(err)
	}
}

// startConversation opens a conversation the team writes first and returns its reply token.
func (f fixture) startConversation(t *testing.T, address string) string {
	t.Helper()
	ctx := context.Background()
	contact, err := f.store.CreateContact(ctx, f.workspaceID, "Quinn Customer", address)
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := f.store.StartConversation(ctx, f.workspaceID, store.NewConversation{ContactID: contact.ID, Subject: "Your ticket"},
		store.Actor{AgentID: f.agentID}, "We moved our help desk.", nil)
	if err != nil {
		t.Fatal(err)
	}
	return queryOne(t, f, `SELECT substr(key,13) FROM conversation_thread_keys WHERE conversation_id='`+conversation.ID+`' AND channel_type='email'`)
}

// Before the team's mailbox forwards to Colliedesk, the support address still delivers to the old help
// desk, so a customer's answer to it never comes back. Until mail has arrived through the support
// mailbox, a server with a platform mail domain asks for the answer at the conversation's platform reply
// address, which always reaches Colliedesk. A test message or an answer to that platform address does not
// prove forwarding. The From line never changes.
func TestReplyToIsThePlatformAddressUntilMailArrivesThroughTheSupportMailbox(t *testing.T) {
	forwarded := "From: Olive Customer <olive@customer.example>\r\nTo: support@waiting.example.com\r\n" +
		"Subject: Broken export\r\nMessage-ID: <forwarded-1@customer.example>\r\n\r\nExport fails.\r\n"
	testMessage := "From: Admin <admin@waiting.example.com>\r\nTo: support@waiting.example.com\r\n" +
		"Subject: Test message from Colliedesk\r\nMessage-ID: <test-1@waiting.example.com>\r\n\r\nA test.\r\n"
	// Gmail's forwarding confirmation reaches the hosted address before the mailbox forwards anything.
	gmailConfirmation := "From: Gmail Team <forwarding-noreply@google.com>\r\nTo: support@waiting.example.com\r\n" +
		"Subject: (#563947195) Gmail Forwarding Confirmation - Receive Mail from support@waiting.example.com\r\n" +
		"Message-ID: <confirmation-1@google.com>\r\n\r\nConfirmation code: 563947195\r\n"
	bulk := "From: News <news@vendor.example>\r\nTo: support@waiting.example.com\r\nPrecedence: bulk\r\n" +
		"Subject: Our newsletter\r\nMessage-ID: <bulk-1@vendor.example>\r\n\r\nNews.\r\n"
	platformAnswer := "From: Rae Customer <rae@customer.example>\r\nTo: reply+0123456789abcdef0123456789abcdef@mail.colliedesk.test\r\n" +
		"Subject: Re: Hello\r\nMessage-ID: <platform-answer-1@customer.example>\r\n\r\nThanks.\r\n"
	// receives is a server that takes mail for its platform mail domain (MAIL_FROM_INBOUND_SECRET).
	type server struct {
		mailFromDomain string
		receives       bool
	}
	platform, unreceived, selfHosted := server{"mail.colliedesk.test", true}, server{"mail.colliedesk.test", false}, server{"", false}
	cases := []struct {
		name        string
		server      server
		arrived     func(t *testing.T, f fixture)
		wantReplyTo string
	}{
		{"nothing arrived yet", platform, nil, `"Waiting Desk" <reply+TOKEN@mail.colliedesk.test>`},
		{"only a test message arrived", platform, func(t *testing.T, f fixture) { f.arrive(t, testMessage, false, true) },
			`"Waiting Desk" <reply+TOKEN@mail.colliedesk.test>`},
		{"only an answer to a platform reply address arrived", platform, func(t *testing.T, f fixture) { f.arrive(t, platformAnswer, true, true) },
			`"Waiting Desk" <reply+TOKEN@mail.colliedesk.test>`},
		{"customer mail arrived through forwarding", platform, func(t *testing.T, f fixture) { f.arrive(t, forwarded, false, false) },
			`"Waiting Desk" <support+r-TOKEN@waiting.example.com>`},
		{"customer mail arrived through the platform forwarder", platform, func(t *testing.T, f fixture) { f.arrive(t, forwarded, true, false) },
			`"Waiting Desk" <support+r-TOKEN@waiting.example.com>`},
		{"only Gmail's forwarding confirmation arrived", platform, func(t *testing.T, f fixture) { f.arrive(t, gmailConfirmation, true, false) },
			`"Waiting Desk" <reply+TOKEN@mail.colliedesk.test>`},
		{"only dropped bulk mail arrived", platform, func(t *testing.T, f fixture) { f.arrive(t, bulk, true, false) },
			`"Waiting Desk" <reply+TOKEN@mail.colliedesk.test>`},
		{"customer mail arrived under the previous support address", platform, func(t *testing.T, f fixture) {
			f.supportAddress(t, "old@waiting.example.com")
			f.arrive(t, forwarded, false, false)
			f.supportAddress(t, "support@waiting.example.com")
		}, `"Waiting Desk" <reply+TOKEN@mail.colliedesk.test>`},
		{"customer mail arrived and the same support address was saved again", platform, func(t *testing.T, f fixture) {
			f.arrive(t, forwarded, false, false)
			f.supportAddress(t, "support@waiting.example.com")
		}, `"Waiting Desk" <support+r-TOKEN@waiting.example.com>`},
		{"no platform mail domain", selfHosted, nil, `"Waiting Desk" <support+r-TOKEN@waiting.example.com>`},
		{"a platform mail domain nothing receives mail for", unreceived, nil, `"Waiting Desk" <support+r-TOKEN@waiting.example.com>`},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			f := waitingDesk(t)
			if item.arrived != nil {
				item.arrived(t, f)
			}
			token := f.startConversation(t, "quinn@customer.example")
			relay := startSMTPServer(t)
			clock := time.Now().UTC().Add(time.Second)
			process(t, newWorker(f, relay, &clock, func(options *emailoutbox.Options) {
				options.MailFromDomain, options.ReceivesPlatformReplies = item.server.mailFromDomain, item.server.receives
			}))
			received := relay.mail()
			if len(received) != 1 {
				t.Fatalf("want one mail, got %d", len(received))
			}
			parsed, err := mail.ReadMessage(strings.NewReader(received[0].Data))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := parsed.Header.Get("Reply-To"), strings.ReplaceAll(item.wantReplyTo, "TOKEN", token); got != want {
				t.Errorf("Reply-To=%q, want %q", got, want)
			}
			if got, want := parsed.Header.Get("From"), `"Waiting Desk" <support@waiting.example.com>`; got != want {
				t.Errorf("From=%q, want %q", got, want)
			}
		})
	}
}
