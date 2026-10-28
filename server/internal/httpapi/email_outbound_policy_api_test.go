package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/email"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

type domainSettings struct {
	Channel *struct {
		SupportAddress   string  `json:"support_address"`
		Domain           string  `json:"domain"`
		DomainVerifiedAt *string `json:"domain_verified_at"`
		DNSRecordName    string  `json:"dns_record_name"`
		DNSRecordValue   string  `json:"dns_record_value"`
	} `json:"channel"`
	FromPolicy  string  `json:"from_policy"`
	SendingFrom *string `json:"sending_from"`
}

func domainState(t *testing.T, api http.Handler, token string) domainSettings {
	t.Helper()
	var settings domainSettings
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels/email", nil, token, nil), &settings)
	return settings
}

func verifiedPolicy(mailFromDomain string) func(*httpapi.Config) {
	return func(config *httpapi.Config) {
		config.Email.FromPolicy = email.FromPolicyVerified
		config.Email.MailFromDomain = mailFromDomain
	}
}

func TestDomainVerificationLifecycle(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, verifiedPolicy("mail.colliedesk.test"))
	admin := createSession(t, api, service, "Verify Desk", "admin@verify.example.com")
	_ = connectEmail(t, api, admin, "support@verify.example.com")
	settings := domainState(t, api, admin)
	channel := settings.Channel
	if settings.FromPolicy != "verified" || channel.Domain != "verify.example.com" || channel.DomainVerifiedAt != nil ||
		channel.DNSRecordName != "_colliedesk.verify.example.com" || !strings.HasPrefix(channel.DNSRecordValue, "colliedesk-verification=") {
		t.Fatalf("unexpected verification state: %+v %+v", settings, channel)
	}
	if settings.SendingFrom == nil || !strings.HasSuffix(*settings.SendingFrom, "@mail.colliedesk.test") {
		t.Fatalf("unverified domain must send from the platform domain: %v", settings.SendingFrom)
	}

	const verifyPath = "/api/v1/channels/email/domain-verification"
	if response := request(t, api, http.MethodPost, verifyPath, nil, admin, nil); response.Code != http.StatusConflict ||
		!strings.Contains(response.Body.String(), channel.DNSRecordName) {
		t.Fatalf("missing TXT record status=%d body=%s, want 409 naming the record", response.Code, response.Body.String())
	}
	dns.set(channel.DNSRecordName, "v=spf1 -all", channel.DNSRecordValue)
	if response := request(t, api, http.MethodPost, verifyPath, nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", response.Code, response.Body.String())
	}
	settings = domainState(t, api, admin)
	if settings.Channel.DomainVerifiedAt == nil || settings.SendingFrom == nil || *settings.SendingFrom != "support@verify.example.com" {
		t.Fatalf("verified domain must send as the support address: %+v %v", settings.Channel, settings.SendingFrom)
	}

	// The verification belongs to the domain: another address on it keeps it, another domain starts over.
	_ = request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "help@verify.example.com"}, admin, nil)
	settings = domainState(t, api, admin)
	if settings.Channel.DomainVerifiedAt == nil || settings.Channel.DNSRecordValue != channel.DNSRecordValue {
		t.Fatalf("another address on the verified domain lost its verification: %+v", settings.Channel)
	}
	_ = request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "help@verify-two.example.com"}, admin, nil)
	settings = domainState(t, api, admin)
	if settings.Channel.DomainVerifiedAt != nil || settings.Channel.DNSRecordValue == channel.DNSRecordValue || settings.Channel.DNSRecordName != "_colliedesk.verify-two.example.com" {
		t.Fatalf("an address on another domain must start a new verification: %+v", settings.Channel)
	}
}

func TestDomainVerificationIsWorkspaceScoped(t *testing.T) {
	api, service, _, dns := testAPIWithConfig(t, verifiedPolicy("mail.colliedesk.test"))
	adminA := createSession(t, api, service, "Verify Alpha", "admin@verify-alpha.example.com")
	adminB := createSession(t, api, service, "Verify Beta", "admin@verify-beta.example.com")
	_ = connectEmail(t, api, adminA, "support@shared.example.com")
	_ = connectEmail(t, api, adminB, "support@shared.example.com")
	recordB := domainState(t, api, adminB).Channel
	dns.set(recordB.DNSRecordName, recordB.DNSRecordValue)
	if response := request(t, api, http.MethodPost, "/api/v1/channels/email/domain-verification", nil, adminA, nil); response.Code != http.StatusConflict {
		t.Fatalf("workspace B's TXT record verified workspace A: status=%d", response.Code)
	}
	if response := request(t, api, http.MethodPost, "/api/v1/channels/email/domain-verification", nil, adminB, nil); response.Code != http.StatusOK {
		t.Fatalf("B verify status=%d", response.Code)
	}
	if domainState(t, api, adminA).Channel.DomainVerifiedAt != nil {
		t.Fatal("verifying B marked A verified")
	}
}

func replyDelivery(t *testing.T, api http.Handler, admin, secret string) (string, *struct {
	Status   string `json:"status"`
	Attempts int    `json:"attempts"`
}, string) {
	t.Helper()
	thread := inboundOK(t, api, secret, mail([]string{"From: u@customer.example", "Subject: Policy", "Message-ID: <policy@customer.example>"}, "Hi."))
	reply := create(t, api, admin, "/api/v1/conversations/"+*thread.ConversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Answer."})
	response := request(t, api, http.MethodGet, "/api/v1/conversations/"+*thread.ConversationID+"/messages", nil, admin, nil)
	items := timeline(t, api, admin, *thread.ConversationID)
	return reply, items[1].Delivery, response.Body.String()
}

func TestReplyStateFollowsOutboundReadiness(t *testing.T) {
	cases := []struct {
		name, wantStatus, wantError string
		mutate                      func(*httpapi.Config)
	}{
		{"smtp not configured", "failed", "Outgoing email is not configured", func(c *httpapi.Config) { c.Email.SMTP = email.SMTPConfig{} }},
		{"unverified without platform domain", "failed", "verify the support address domain", verifiedPolicy("")},
		{"unverified with platform domain", "queued", "", verifiedPolicy("mail.colliedesk.test")},
		{"any policy", "queued", "", nil},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			api, service, _, _ := testAPIWithConfig(t, item.mutate)
			admin := createSession(t, api, service, "Ready Desk", "admin@ready.example.com")
			secret := connectEmail(t, api, admin, "support@ready.example.com")
			reply, delivery, body := replyDelivery(t, api, admin, secret)
			if delivery == nil || delivery.Status != item.wantStatus || !strings.Contains(strings.ToLower(body), strings.ToLower(item.wantError)) {
				t.Fatalf("delivery=%+v body=%s, want %s %q", delivery, body, item.wantStatus, item.wantError)
			}
			if item.wantStatus == "failed" {
				retry := request(t, api, http.MethodPost, "/api/v1/conversations/"+queryConversation(t, body)+"/messages/"+reply+"/retry", nil, admin, nil)
				if retry.Code != http.StatusConflict {
					t.Fatalf("retry while outbound cannot send status=%d, want 409", retry.Code)
				}
			}
		})
	}
}

func queryConversation(t *testing.T, timelineBody string) string {
	t.Helper()
	const key = `"conversation_id":"`
	start := strings.Index(timelineBody, key)
	if start < 0 {
		t.Fatalf("no conversation id in %s", timelineBody)
	}
	return timelineBody[start+len(key) : start+len(key)+36]
}
