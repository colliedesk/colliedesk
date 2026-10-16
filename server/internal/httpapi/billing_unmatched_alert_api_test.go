package httpapi_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

type sentMail struct {
	From    string
	To      []string
	Message string
}

// recordingMailer stands in for the SMTP relay, the only boundary faked here.
type recordingMailer struct {
	mu   sync.Mutex
	sent []sentMail
}

func (m *recordingMailer) Send(_ context.Context, from string, to []string, message []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMail{From: from, To: to, Message: string(message)})
	return nil
}

func (m *recordingMailer) all() []sentMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentMail(nil), m.sent...)
}

func newAlertFixture(t *testing.T, alertTo string) (billingFixture, *recordingMailer) {
	t.Helper()
	mailer := &recordingMailer{}
	fake := newFakePolarAPI(t)
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Billing = fake.config()
		config.Email.MailFromDomain = "mail.example.com"
		config.AlertEmail = alertTo
		config.AlertMailer = mailer
	})
	adminA := createSession(t, api, service, "Alert Alpha", "admin@alert-alpha.example.com")
	return billingFixture{api: api, service: service, pool: pool, fake: fake, adminA: adminA, workspaceA: workspaceID(t, api, adminA)}, mailer
}

func foundingBuyer(id, status string) subscription {
	now := time.Now().UTC().Truncate(time.Second)
	return subscription{ID: id, Product: productFounding, Status: status, PeriodEnd: now.Add(365 * 24 * time.Hour),
		ModifiedAt: now, CustomerEmail: "buyer@paid.example.org"}
}

func TestUnmatchedActiveSubscriptionAlertsTheOperator(t *testing.T) {
	f, mailer := newAlertFixture(t, "ops@alerts.example.net")
	response := f.deliver(t, "msg_unmatched", foundingBuyer("sub_orphan", "active").event("subscription.active"))
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"ignored"`) {
		t.Fatalf("status=%d body=%s, want 200 ignored", response.Code, response.Body.String())
	}
	sent := mailer.all()
	if len(sent) != 1 {
		t.Fatalf("sent %d alerts, want 1", len(sent))
	}
	mail := sent[0]
	if mail.From != "noreply@mail.example.com" || len(mail.To) != 1 || mail.To[0] != "ops@alerts.example.net" {
		t.Fatalf("from=%q to=%v, want noreply@mail.example.com -> ops@alerts.example.net", mail.From, mail.To)
	}
	for _, want := range []string{"buyer@paid.example.org", "sub_orphan", "cus_sub_orphan", "founding"} {
		if !strings.Contains(mail.Message, want) {
			t.Errorf("alert lacks %q:\n%s", want, mail.Message)
		}
	}
	if strings.Contains(mail.Message, billingWebhookSecret) || strings.Contains(mail.Message, "polar-token-for-test") {
		t.Fatalf("alert leaks a secret:\n%s", mail.Message)
	}
}

func TestUnmatchedAlertAlsoCoversAWorkspaceIDThatDoesNotExist(t *testing.T) {
	f, mailer := newAlertFixture(t, "ops@alerts.example.net")
	sub := foundingBuyer("sub_ghost", "active")
	sub.Workspace = "11111111-2222-3333-4444-555555555555"
	f.mustDeliver(t, "msg_ghost", sub.event("subscription.active"))
	if sent := mailer.all(); len(sent) != 1 || !strings.Contains(sent[0].Message, "sub_ghost") {
		t.Fatalf("sent=%v, want one alert naming sub_ghost", sent)
	}
}

func TestUnmatchedAlertIsSentOncePerSubscription(t *testing.T) {
	f, mailer := newAlertFixture(t, "ops@alerts.example.net")
	body := foundingBuyer("sub_dup", "active")
	f.mustDeliver(t, "msg_one", body.event("subscription.created"))
	f.mustDeliver(t, "msg_two", body.event("subscription.active"))
	f.mustDeliver(t, "msg_two", body.event("subscription.active"))
	if sent := mailer.all(); len(sent) != 1 {
		t.Fatalf("sent %d alerts for one subscription, want 1", len(sent))
	}
}

func TestNoAlertWhenNothingWasPaidOrAWorkspaceMatched(t *testing.T) {
	f, mailer := newAlertFixture(t, "ops@alerts.example.net")
	f.mustDeliver(t, "msg_incomplete", foundingBuyer("sub_new", "incomplete").event("subscription.created"))
	f.mustDeliver(t, "msg_canceled", foundingBuyer("sub_old", "canceled").event("subscription.canceled"))
	matched := foundingBuyer("sub_ok", "active")
	matched.Workspace = f.workspaceA
	f.mustDeliver(t, "msg_matched", matched.event("subscription.active"))
	if sent := mailer.all(); len(sent) != 0 {
		t.Fatalf("sent %d alerts, want none: %v", len(sent), sent)
	}
}

func TestUnmatchedPaymentWithoutAlertAddressStillAnswers200(t *testing.T) {
	f, mailer := newAlertFixture(t, "")
	response := f.deliver(t, "msg_noaddr", foundingBuyer("sub_quiet", "active").event("subscription.active"))
	if response.Code != 200 || len(mailer.all()) != 0 {
		t.Fatalf("status=%d sent=%d, want 200 and no mail", response.Code, len(mailer.all()))
	}
}
