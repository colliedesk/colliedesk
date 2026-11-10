package httpapi_test

import (
	"context"
	"net/http"
	netmail "net/mail"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels/registry"
	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// relayStandIn is the SMTP relay the outbox worker hands replies to; it keeps each composed message.
type relayStandIn struct{ messages []string }

func (r *relayStandIn) SendCopying(_ context.Context, _, _ string, _ []string, message []byte) ([]email.RefusedCopy, error) {
	r.messages = append(r.messages, string(message))
	return nil, nil
}

// sendQueuedReplies runs the email outbox once, as the server does with platformConfig's mail domain and
// inbound secret, and returns the Reply-To of each email it sent.
func sendQueuedReplies(t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	relay := &relayStandIn{}
	worker := emailoutbox.New(store.New(pool).WithChannels(registry.New(registry.Config{})), relay,
		emailoutbox.Options{FromPolicy: email.FromPolicyAny, MailFromDomain: "mail.colliedesk.test", ReceivesPlatformReplies: true})
	if _, err := worker.ProcessDue(context.Background()); err != nil {
		t.Fatal(err)
	}
	var replyTo []string
	for _, raw := range relay.messages {
		parsed, err := netmail.ReadMessage(strings.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		replyTo = append(replyTo, parsed.Header.Get("Reply-To"))
	}
	return replyTo
}

func answer(t *testing.T, api http.Handler, admin, conversation, body string) {
	t.Helper()
	create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": body})
}

// While the mailbox still forwards to the old help desk, the customer answers the platform Reply-To. The
// answer threads into the conversation, and it does not count as forwarding: the next reply still asks
// for answers at the platform address. Mail through the support mailbox then switches replies back to it.
func TestAnAnswerToThePlatformReplyToThreadsIntoTheSameConversation(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, platformConfig)
	admin := createSession(t, api, service, "Early Desk", "admin@early.example.com")
	secret := connectEmail(t, api, admin, "support@early.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Quinn", "email": "quinn@customer.example"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "We moved"})
	answer(t, api, admin, conversation, "We answer from Colliedesk now.")
	token := queryString(t, pool, `SELECT substr(key,13) FROM conversation_thread_keys WHERE conversation_id=$1 AND channel_type='email'`, conversation)
	platform := `"Early Desk" <reply+` + token + `@mail.colliedesk.test>`
	if got := sendQueuedReplies(t, pool); len(got) != 1 || got[0] != platform {
		t.Fatalf("Reply-To before forwarding = %q, want [%s]", got, platform)
	}

	result := inboundOK(t, api, platformSecret, mail([]string{"X-Original-To: reply+" + token + "@mail.colliedesk.test",
		"From: Quinn <quinn@customer.example>", "To: \"Early Desk\" <reply+" + token + "@mail.colliedesk.test>",
		"Subject: Re: We moved", "Message-ID: <quinn-answer@customer.example>"}, "Got it."))
	if result.Status != "threaded" || result.ConversationID == nil || *result.ConversationID != conversation {
		t.Fatalf("answer to the platform Reply-To = %+v, want threaded into %s", result, conversation)
	}
	answer(t, api, admin, conversation, "Glad it works.")
	if got := sendQueuedReplies(t, pool); len(got) != 1 || got[0] != platform {
		t.Fatalf("Reply-To after an answer to the platform address = %q, want [%s]", got, platform)
	}

	forwarded := inboundOK(t, api, secret, mail([]string{"From: Olive <olive@customer.example>", "To: support@early.example.com",
		"Subject: New question", "Message-ID: <olive-forwarded@customer.example>"}, "Forwarded at last."))
	if forwarded.Status != "created" {
		t.Fatalf("forwarded mail = %+v, want created", forwarded)
	}
	answer(t, api, admin, conversation, "Back on the support address.")
	if got, want := sendQueuedReplies(t, pool), `"Early Desk" <support+r-`+token+`@early.example.com>`; len(got) != 1 || got[0] != want {
		t.Fatalf("Reply-To after forwarded mail = %q, want [%s]", got, want)
	}
}

// "Send yourself a test message" goes into the inbox without passing the support mailbox, so it proves
// nothing about forwarding: the answer to it still asks for replies at the platform address.
func TestATestMessageDoesNotCountAsMailThroughForwarding(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, platformConfig)
	admin := createSession(t, api, service, "Trying Desk", "admin@trying.example.com")
	connectEmail(t, api, admin, "support@trying.example.com")
	got, sent := sendTestMessage(t, api, admin, "/api/v1/channels/email/test-message")
	if got.Code != http.StatusCreated {
		t.Fatalf("test message: status=%d body=%s", got.Code, got.Body)
	}
	answer(t, api, admin, sent.ConversationID, "Looks good.")
	token := queryString(t, pool, `SELECT substr(key,13) FROM conversation_thread_keys WHERE conversation_id=$1 AND channel_type='email'`, sent.ConversationID)
	if replies, want := sendQueuedReplies(t, pool), `"Trying Desk" <reply+`+token+`@mail.colliedesk.test>`; len(replies) != 1 || replies[0] != want {
		t.Fatalf("Reply-To after only a test message = %q, want [%s]", replies, want)
	}
}
