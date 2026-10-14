package emailoutbox_test

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (f fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func TestStaleFinishAfterLeaseExpiryCannotOverwriteSent(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock)
	reply := f.write(t, "outgoing", "Once.")
	stale, err := f.store.ClaimDueDeliveries(context.Background(), clock, 1, time.Minute)
	if err != nil || len(stale) != 1 {
		t.Fatalf("claim: %d %v", len(stale), err)
	}
	clock = clock.Add(2 * time.Minute)
	if process(t, worker) != 1 || f.delivery(t, reply.ID).Status != "sent" {
		t.Fatal("reclaimed delivery was not sent")
	}
	err = f.store.MarkDeliveryFailed(context.Background(), stale[0], "stale worker timed out")
	if !errors.Is(err, store.ErrLeaseLost) {
		t.Fatalf("stale finish err=%v, want ErrLeaseLost", err)
	}
	if delivery := f.delivery(t, reply.ID); delivery.Status != "sent" || delivery.LastError != nil {
		t.Fatalf("stale finish overwrote the sent state: %+v", delivery)
	}
}

func TestPoisonRowDoesNotStallOthersAndAttemptsAreCapped(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock)
	poison := f.write(t, "outgoing", "To a broken contact.")
	f.exec(t, `UPDATE contacts SET email='not an address' WHERE workspace_id=$1`, f.workspaceID)
	other := f.secondConversationReply(t, "healthy@customer.example", "Healthy reply.")
	process(t, worker)
	process(t, worker)
	if delivery := f.delivery(t, poison.ID); delivery.Status != "failed" || delivery.LastError == nil {
		t.Fatalf("poison row: %+v", delivery)
	}
	if delivery := f.deliveryIn(t, other.ConversationID, other.ID); delivery.Status != "sent" {
		t.Fatalf("healthy row stalled behind the poison row: %+v", delivery)
	}

	crashLoop := f.secondConversationReply(t, "loop@customer.example", "Crash loop.")
	f.exec(t, `UPDATE email_outbox SET attempts=3,locked_until=$2 WHERE message_id=$1`, crashLoop.ID, clock.Add(-time.Second))
	before := len(server.mail())
	process(t, worker)
	if delivery := f.deliveryIn(t, crashLoop.ConversationID, crashLoop.ID); delivery.Status != "failed" || len(server.mail()) != before {
		t.Fatalf("a row that already used every attempt must fail without sending: %+v", delivery)
	}
}

func TestSendingIdentityFollowsDomainVerification(t *testing.T) {
	cases := []struct {
		name, verified, wantFrom, wantEnvelope string
		policy                                 string
	}{
		{"any policy", "", `"Outbound Desk" <support@outbound.example.com>`, "support@outbound.example.com", email.FromPolicyAny},
		{"verified domain", "now()", `"Outbound Desk" <support@outbound.example.com>`, "support@outbound.example.com", email.FromPolicyVerified},
		{"unverified domain", "", `"Outbound Desk via CollieDesk" <reply+TOKEN@mail.colliedesk.test>`, "reply+TOKEN@mail.colliedesk.test", email.FromPolicyVerified},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			f := newFixture(t)
			if item.verified != "" {
				f.exec(t, `UPDATE email_channels SET domain_verified_at=now() WHERE workspace_id=$1`, f.workspaceID)
			}
			server := startSMTPServer(t)
			clock := time.Now().UTC().Add(time.Second)
			worker := newWorker(f, server, &clock, func(options *emailoutbox.Options) {
				options.FromPolicy, options.MailFromDomain = item.policy, "mail.colliedesk.test"
			})
			reply := f.write(t, "outgoing", "Identity.")
			process(t, worker)
			received := server.mail()
			if len(received) != 1 {
				t.Fatalf("want one mail, got %d (delivery %+v)", len(received), f.delivery(t, reply.ID))
			}
			var token string
			if err := f.pool.QueryRow(context.Background(), `SELECT email_reply_token FROM conversations WHERE id=$1`, f.conversationID).Scan(&token); err != nil {
				t.Fatal(err)
			}
			parsed, err := mail.ReadMessage(strings.NewReader(received[0].Data))
			if err != nil {
				t.Fatal(err)
			}
			if got, want := parsed.Header.Get("From"), strings.ReplaceAll(item.wantFrom, "TOKEN", token); got != want {
				t.Errorf("From=%q, want %q", got, want)
			}
			if got, want := received[0].From, strings.ReplaceAll(item.wantEnvelope, "TOKEN", token); got != want {
				t.Errorf("envelope sender=%q, want %q", got, want)
			}
			if got, want := parsed.Header.Get("Reply-To"), `"Outbound Desk" <support+r-`+token+`@outbound.example.com>`; got != want {
				t.Errorf("Reply-To=%q, want %q", got, want)
			}
		})
	}
}

func TestWorkspaceHourlySendingLimitDefersWithoutSpendingAttempts(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock, func(options *emailoutbox.Options) { options.HourlyLimit = 1 })
	first := f.write(t, "outgoing", "First.")
	second := f.write(t, "outgoing", "Second.")
	process(t, worker)
	process(t, worker)
	if f.delivery(t, first.ID).Status != "sent" {
		t.Fatal("first reply not sent")
	}
	delivery := f.delivery(t, second.ID)
	if delivery.Status != "queued" || delivery.Attempts != 0 || delivery.LastError == nil || !strings.Contains(*delivery.LastError, "hourly") {
		t.Fatalf("second reply must wait for the hourly limit: %+v", delivery)
	}
	if len(server.mail()) != 1 {
		t.Fatalf("limit exceeded: %d mails sent", len(server.mail()))
	}
}

func TestHeaderValuesCannotInjectHeaders(t *testing.T) {
	f := newFixture(t)
	f.exec(t, `UPDATE workspaces SET name=E'Desk\r\nBcc: evil@attacker.example' WHERE id=$1`, f.workspaceID)
	f.exec(t, `UPDATE contacts SET name=E'Olive\r\nBcc: evil@attacker.example' WHERE workspace_id=$1`, f.workspaceID)
	f.exec(t, `UPDATE conversations SET subject=E'Broken\r\nBcc: evil@attacker.example\r\n\r\nInjected body' WHERE id=$1`, f.conversationID)
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock)
	f.write(t, "outgoing", "Safe.")
	process(t, worker)
	received := server.mail()
	if len(received) != 1 || len(received[0].To) != 1 {
		t.Fatalf("unexpected envelope: %+v", received)
	}
	head, _, _ := strings.Cut(received[0].Data, "\r\n\r\n")
	var names []string
	for _, line := range strings.Split(head, "\r\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		name, _, _ := strings.Cut(line, ":")
		names = append(names, name)
	}
	want := "From,To,Reply-To,Subject,Date,Message-ID,In-Reply-To,References,MIME-Version,Content-Type,Content-Transfer-Encoding"
	if strings.Join(names, ",") != want {
		t.Fatalf("header names=%v, want %s\n%s", names, want, head)
	}
	if strings.Contains(head, "\r\nBcc") || strings.Contains(received[0].Data, "\r\n\r\nInjected body") {
		t.Fatalf("injection reached the message:\n%s", received[0].Data)
	}
}

func TestSentAtIsTheSendTime(t *testing.T) {
	f := newFixture(t)
	server := startSMTPServer(t)
	start := time.Now().UTC().Add(time.Second)
	calls := 0
	sender := email.SMTPSender{Config: email.SMTPConfig{Host: "127.0.0.1", Port: server.port()}}
	worker := emailoutbox.New(f.store, sender, emailoutbox.Options{FromPolicy: email.FromPolicyAny, Now: func() time.Time {
		calls++
		return start.Add(time.Duration(calls) * time.Minute)
	}})
	reply := f.write(t, "outgoing", "When?")
	process(t, worker)
	delivery := f.delivery(t, reply.ID)
	if delivery.SentAt == nil || !delivery.SentAt.After(start.Add(time.Minute)) {
		t.Fatalf("sent_at=%v must be read after sending, not at claim time %v", delivery.SentAt, start.Add(time.Minute))
	}
}
