package emailoutbox_test

import (
	"bytes"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/emailoutbox"
)

// The workspace name is an internal label; customers see the channel's sender name on every
// transport, in From and in Reply-To.
func TestEveryTransportSendsAsTheChannelSenderNameNotTheWorkspaceName(t *testing.T) {
	relay := []struct {
		name, verified, wantFrom string
	}{
		{"relay with a verified domain", "now()", `"Colliedesk Support" <support@outbound.example.com>`},
		{"relay with an unverified domain", "", `"Colliedesk Support via Colliedesk" <reply+`},
	}
	for _, item := range relay {
		t.Run(item.name, func(t *testing.T) {
			f := newFixture(t)
			f.exec(t, `UPDATE email_channels SET sender_name='Colliedesk Support' WHERE workspace_id=$1`, f.workspaceID)
			f.exec(t, `UPDATE email_domains SET verified_at=`+orNull(item.verified)+` WHERE workspace_id=$1`, f.workspaceID)
			server := startSMTPServer(t)
			clock := time.Now().UTC().Add(time.Second)
			worker := newWorker(f, server, &clock, func(options *emailoutbox.Options) {
				options.FromPolicy, options.MailFromDomain = email.FromPolicyVerified, "mail.colliedesk.test"
			})
			reply := f.write(t, "outgoing", "Named by the channel.")
			process(t, worker)
			received := server.mail()
			if len(received) != 1 {
				t.Fatalf("want one mail, got %d (delivery %+v)", len(received), f.delivery(t, reply.ID))
			}
			parsed, err := mail.ReadMessage(strings.NewReader(received[0].Data))
			if err != nil {
				t.Fatal(err)
			}
			assertSenderName(t, parsed, item.wantFrom)
		})
	}

	t.Run("gmail", func(t *testing.T) {
		f := newFixture(t)
		f.exec(t, `UPDATE email_channels SET sender_name='Colliedesk Support' WHERE workspace_id=$1`, f.workspaceID)
		fake, config := gmailStandIn(t)
		f.connectGmail(t, fake, config, "help@acme.example")
		smtp := startSMTPServer(t)
		clock := time.Now().UTC().Add(time.Second)
		worker := gmailWorker(f, smtp, config, &clock)
		reply := f.write(t, "outgoing", "Named by the channel.")
		process(t, worker)
		sent := fake.Messages()
		if len(sent) != 1 {
			t.Fatalf("Gmail messages = %d (delivery %+v)", len(sent), f.delivery(t, reply.ID))
		}
		parsed, err := mail.ReadMessage(bytes.NewReader(sent[0].Raw))
		if err != nil {
			t.Fatal(err)
		}
		assertSenderName(t, parsed, `"Colliedesk Support" <help@acme.example>`)
	})
}

func orNull(value string) string {
	if value == "" {
		return "NULL"
	}
	return value
}

func assertSenderName(t *testing.T, parsed *mail.Message, wantFrom string) {
	t.Helper()
	if got := parsed.Header.Get("From"); !strings.HasPrefix(got, wantFrom) {
		t.Errorf("From=%q, want it to start with %q", got, wantFrom)
	}
	replyTo, err := mail.ParseAddress(parsed.Header.Get("Reply-To"))
	if err != nil || replyTo.Name != "Colliedesk Support" {
		t.Errorf("Reply-To=%q, want the name Colliedesk Support", parsed.Header.Get("Reply-To"))
	}
	if strings.Contains(parsed.Header.Get("From")+parsed.Header.Get("Reply-To"), "Outbound Desk") {
		t.Error("the workspace name reached the customer")
	}
}

// A sender name with Vietnamese diacritics reaches the customer exactly, in From and in Reply-To.
func TestAVietnameseSenderNameIsReadBackExactly(t *testing.T) {
	const name = "Hỗ trợ khách hàng"
	f := newFixture(t)
	f.exec(t, `UPDATE email_channels SET sender_name=$2 WHERE workspace_id=$1`, f.workspaceID, name)
	f.exec(t, `UPDATE email_domains SET verified_at=now() WHERE workspace_id=$1`, f.workspaceID)
	server := startSMTPServer(t)
	clock := time.Now().UTC().Add(time.Second)
	worker := newWorker(f, server, &clock, func(options *emailoutbox.Options) {
		options.FromPolicy, options.MailFromDomain = email.FromPolicyVerified, "mail.colliedesk.test"
	})
	reply := f.write(t, "outgoing", "Xin chào.")
	process(t, worker)
	received := server.mail()
	if len(received) != 1 {
		t.Fatalf("want one mail, got %d (delivery %+v)", len(received), f.delivery(t, reply.ID))
	}
	parsed, err := mail.ReadMessage(strings.NewReader(received[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"From", "Reply-To"} {
		address, err := mail.ParseAddress(parsed.Header.Get(field))
		if err != nil || address.Name != name {
			t.Errorf("%s=%q read back as %+v (err %v), want the name %q", field, parsed.Header.Get(field), address, err, name)
		}
	}
}
