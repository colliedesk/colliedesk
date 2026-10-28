package freshdesk_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

func (e *importEnv) mailboxes(t *testing.T, ticket string) []string {
	t.Helper()
	conversation, err := store.New(e.pool).GetConversation(context.Background(), e.workspaceID, e.target(t, "ticket", ticket))
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Origin == nil {
		return nil
	}
	return conversation.Origin.Mailboxes
}

// An imported ticket keeps the mailboxes it was sent to, most trusted first: its email configuration
// (read once), the support address it was answered from, then its recipients; an outbound ticket's
// recipients are customers and do not count.
func TestAnImportedTicketKeepsTheMailboxesItWasSentTo(t *testing.T) {
	env := newImportEnv(t)
	env.fake.ticketEmailConfig = map[string]int64{"1": 11}
	env.fake.emailConfigs = []map[string]any{{"id": 11, "name": "Billing", "to_email": "billing@acme.example", "reply_email": "Billing@Acme.example"}}
	env.mustRun(t)
	if got := fmt.Sprint(env.mailboxes(t, "1")); got != "[billing@acme.example support@acme.example]" {
		t.Errorf("ticket 1 mailboxes = %s", got)
	}
	if got := env.mailboxes(t, "6"); len(got) != 0 {
		t.Errorf("an outbound ticket's mailboxes = %v, want none (its recipients are customers)", got)
	}
	if n := env.fake.count("/api/v2/email_configs"); n != 1 {
		t.Errorf("email configurations were read %d times, want once", n)
	}
}

// An account that refuses its email configurations still imports every ticket, with what else names
// a mailbox, and the report says the configurations could not be read.
func TestEmailConfigurationsTheAccountRefusesDoNotStopTheImport(t *testing.T) {
	env := newImportEnv(t)
	env.fake.ticketEmailConfig = map[string]int64{"1": 11}
	env.fake.emailConfigsStatus = http.StatusForbidden
	report := env.mustRun(t)
	if got := fmt.Sprint(env.mailboxes(t, "1")); got != "[support@acme.example]" {
		t.Errorf("ticket 1 mailboxes = %s", got)
	}
	found := false
	for _, warning := range report.Warnings {
		found = found || warning.Code == "email_configs_unavailable"
	}
	if !found {
		t.Errorf("warnings %+v, want email_configs_unavailable", report.Warnings)
	}
}
