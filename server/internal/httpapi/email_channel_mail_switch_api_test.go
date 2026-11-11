package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

type mailSwitchView struct {
	MailThroughMailbox      bool   `json:"mail_through_mailbox"`
	AnswersBeforeForwarding string `json:"answers_before_forwarding"`
}

func mailSwitch(t *testing.T, api http.Handler, token, path string) mailSwitchView {
	t.Helper()
	response := request(t, api, http.MethodGet, path, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body)
	}
	var view mailSwitchView
	decode(t, response, &view)
	return view
}

// The inbox says that new customer mail still arrives in the old help desk until mail has come through the
// support mailbox. The email channel answers it with the rule the Reply-To uses: a test message does not
// count, forwarded customer mail does, and another workspace's mail never does.
func TestTheEmailChannelSaysWhetherCustomerMailArrivedThroughTheSupportMailbox(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, platformConfig)
	admin := createSession(t, api, service, "Switch Desk", "admin@switch.example.com")
	secret := connectEmail(t, api, admin, "support@switch.example.com")
	other := createSession(t, api, service, "Other Desk", "admin@other-switch.example.com")
	otherSecret := connectEmail(t, api, other, "support@other-switch.example.com")

	want := mailSwitchView{MailThroughMailbox: false, AnswersBeforeForwarding: "colliedesk"}
	if got := mailSwitch(t, api, admin, "/api/v1/channels/email"); got != want {
		t.Fatalf("nothing arrived yet = %+v, want %+v", got, want)
	}
	if got, sent := sendTestMessage(t, api, admin, "/api/v1/channels/email/test-message"); got.Code != http.StatusCreated || sent.ConversationID == "" {
		t.Fatalf("test message: status=%d body=%s", got.Code, got.Body)
	}
	if got := mailSwitch(t, api, admin, "/api/v1/channels/email"); got != want {
		t.Fatalf("only a test message arrived = %+v, want %+v", got, want)
	}
	if result := inboundOK(t, api, otherSecret, mail([]string{"From: Olive <olive@customer.example>", "To: support@other-switch.example.com",
		"Subject: Elsewhere", "Message-ID: <olive-other@customer.example>"}, "For the other desk.")); result.Status != "created" {
		t.Fatalf("other workspace's mail = %+v, want created", result)
	}
	if got := mailSwitch(t, api, admin, "/api/v1/channels/email"); got != want {
		t.Fatalf("after another workspace's forwarded mail = %+v, want %+v", got, want)
	}

	if result := inboundOK(t, api, secret, mail([]string{"From: Quinn <quinn@customer.example>", "To: support@switch.example.com",
		"Subject: First forwarded", "Message-ID: <quinn-forwarded@customer.example>"}, "Hello there.")); result.Status != "created" {
		t.Fatalf("forwarded mail = %+v, want created", result)
	}
	arrived := mailSwitchView{MailThroughMailbox: true, AnswersBeforeForwarding: "colliedesk"}
	if got := mailSwitch(t, api, admin, "/api/v1/channels/email"); got != arrived {
		t.Fatalf("after forwarded customer mail = %+v, want %+v", got, arrived)
	}
	items, _ := listChannels(t, api, admin)
	if got := mailSwitch(t, api, admin, "/api/v1/channels/"+channelOf(t, items, "email").ID+"/email"); got != arrived {
		t.Fatalf("the channel by its id = %+v, want %+v", got, arrived)
	}
}

// A server that does not receive mail on its sending domain cannot bring the customer's answer back
// before the mailbox forwards here: it says the answer goes to the support mailbox.
func TestAServerWithoutPlatformRepliesSaysAnswersGoToTheSupportMailbox(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Email.MailFromDomain = "mail.colliedesk.test" })
	admin := createSession(t, api, service, "Plain Desk", "admin@plain.example.com")
	connectEmail(t, api, admin, "support@plain.example.com")
	want := mailSwitchView{MailThroughMailbox: false, AnswersBeforeForwarding: "support_mailbox"}
	if got := mailSwitch(t, api, admin, "/api/v1/channels/email"); got != want {
		t.Fatalf("no platform inbound = %+v, want %+v", got, want)
	}
}
