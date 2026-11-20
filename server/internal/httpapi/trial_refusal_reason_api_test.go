package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// A customer who did write, but whose mail the trial cannot trust, is told apart from a customer who
// never wrote: the refusal names the real reason and the ways out that work.
func TestTrialRefusalSaysWhyTheCustomersMailDoesNotCount(t *testing.T) {
	t.Run("mail posted to the workspace inbound URL", func(t *testing.T) {
		f := newLimitsFixture(t, true)
		posted := f.receiveViaWorkspaceSecret(t, "posted@customer.example")
		got := f.reply(t, f.adminA, *posted.ConversationID)
		wantStatus(t, got, http.StatusForbidden, restrictedCode, "reply to mail posted to the inbound URL")
		for _, want := range []string{`"reason":"mail_not_via_hosted_address"`, "inbound URL", "forwarding address", "verify your sending domain", "choose a plan"} {
			if !strings.Contains(got.Body, want) {
				t.Fatalf("refusal lacks %q: %s", want, got.Body)
			}
		}
		if strings.Contains(got.Body, "wrote to you first") || strings.Contains(got.Body, "reply_to") {
			t.Fatalf("the customer did write; the refusal must not say otherwise: %s", got.Body)
		}
	})
	t.Run("mail whose sender verification failed", func(t *testing.T) {
		f := newLimitsFixture(t, true)
		failed := inboundOK(t, f.api, platformSecret, mail([]string{
			"X-Original-To: support@limits-alpha." + hostedDomain,
			"From: failed@customer.example", "To: support@limits-alpha.example.com", "Subject: Failed check",
			"Message-ID: <failed-check@customer.example>",
			"Authentication-Results: mx.example.net; spf=fail smtp.mailfrom=customer.example; dkim=none; dmarc=fail header.from=customer.example",
		}, "Please help."))
		got := f.reply(t, f.adminA, *failed.ConversationID)
		wantStatus(t, got, http.StatusForbidden, restrictedCode, "reply to mail that failed sender verification")
		for _, want := range []string{`"reason":"sender_authentication_failed"`, "failed sender verification", "verify your sending domain", "choose a plan"} {
			if !strings.Contains(got.Body, want) {
				t.Fatalf("refusal lacks %q: %s", want, got.Body)
			}
		}
		if strings.Contains(got.Body, "wrote to you first") {
			t.Fatalf("the customer did write; the refusal must not say otherwise: %s", got.Body)
		}
	})
	t.Run("mail read from the connected Gmail inbox", func(t *testing.T) {
		f := newLimitsFixture(t, true)
		read := f.receiveViaWorkspaceSecret(t, "inbox@customer.example")
		// As the inbox reader stores it: the account's owner can put any message in a Gmail inbox.
		if _, err := f.pool.Exec(context.Background(), `UPDATE messages SET email_meta=jsonb_set(email_meta,'{via}','"gmail"') WHERE workspace_id=$1 AND conversation_id=$2`,
			f.workspaceA, *read.ConversationID); err != nil {
			t.Fatal(err)
		}
		got := f.reply(t, f.adminA, *read.ConversationID)
		wantStatus(t, got, http.StatusForbidden, restrictedCode, "reply to mail read from the Gmail inbox")
		for _, want := range []string{`"reason":"mail_not_via_hosted_address"`, "Gmail inbox", "forwarding address", "verify your sending domain"} {
			if !strings.Contains(got.Body, want) {
				t.Fatalf("refusal lacks %q: %s", want, got.Body)
			}
		}
		if strings.Contains(got.Body, "inbound URL") {
			t.Fatalf("mail read from Gmail is not mail posted to the inbound URL: %s", got.Body)
		}
	})
	t.Run("a conversation nobody wrote to keeps its reason", func(t *testing.T) {
		f := newLimitsFixture(t, true)
		got := f.reply(t, f.adminA, f.agentStarted(t))
		wantStatus(t, got, http.StatusForbidden, restrictedCode, "reply to an agent-started conversation")
		if !strings.Contains(got.Body, "wrote to you first") || strings.Contains(got.Body, `"reason"`) {
			t.Fatalf("an agent-started conversation is refused as before, without a reason: %s", got.Body)
		}
	})
}
