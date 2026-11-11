package mcpserver_test

import (
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// An AI agent reads in one call what the inbox tells a person before the mailbox forwards here: whether
// customer mail arrived through the support mailbox yet, and where the customer's answer to a reply goes
// until it has.
func TestTheEmailChannelToolSaysWhetherMailArrivedThroughTheSupportMailbox(t *testing.T) {
	platform := newEnvWith(t, func(config *httpapi.Config) {
		config.Email.MailFromDomain = "mail.colliedesk.test"
		config.Email.PlatformInboundSecret = "platform-inbound-secret-0123456789abcdef"
	})
	w := platform.newWorkspace("Switch Tools Desk", "admin@switch-tools.example.com")
	session := platform.connect(w.adminTier, "")
	platform.ok(session, "colliedesk_email_channel_update", map[string]any{"support_address": "help@switch-tools.example.com"})
	got := platform.ok(session, "colliedesk_email_channel_get", nil)
	if at(t, got, "email_channel", "mail_through_mailbox") != false || at(t, got, "email_channel", "answers_before_forwarding") != "colliedesk" {
		t.Fatalf("before forwarding, on a server that receives platform replies = %v", got)
	}
	// The same list the inbox line names: every mailbox of the team's own whose customer mail does not arrive yet.
	if at(t, got, "email_channel", "mail_switch", "imported") != false || at(t, got, "email_channel", "mail_switch", "mailboxes", 0, "support_address") != "help@switch-tools.example.com" ||
		at(t, got, "email_channel", "mail_switch", "mailboxes", 0, "name") == nil {
		t.Fatalf("mail switch before forwarding = %v", at(t, got, "email_channel"))
	}

	plain := newEnv(t)
	own := plain.newWorkspace("Switch Tools Plain", "admin@switch-tools-plain.example.com")
	ownSession := plain.connect(own.adminTier, "")
	plain.ok(ownSession, "colliedesk_email_channel_update", map[string]any{"support_address": "help@switch-tools-plain.example.com"})
	if got := plain.ok(ownSession, "colliedesk_email_channel_get", nil); at(t, got, "email_channel", "answers_before_forwarding") != "support_mailbox" {
		t.Fatalf("before forwarding, on a server without platform replies = %v", got)
	}
}
