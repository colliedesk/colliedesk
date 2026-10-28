package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

const hostedDomain = "desk.colliedesk.test"

func hostedConfig(config *httpapi.Config) {
	config.Email.HostedMailDomain = hostedDomain
	config.Email.PlatformInboundSecret = platformSecret
}

type emailSettingsBody struct {
	HostedAddress *string `json:"hosted_address"`
}

func hostedAddressOf(t *testing.T, api http.Handler, admin string) *string {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/channels/email", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET email channel status=%d", response.Code)
	}
	var body emailSettingsBody
	decode(t, response, &body)
	return body.HostedAddress
}

func hostedMail(envelope, messageID string, extra ...string) string {
	headers := append([]string{"X-Original-To: " + envelope, "From: z@customer.example", "Subject: Hello", "Message-ID: <" + messageID + ">"}, extra...)
	return mail(headers, "Body of "+messageID)
}

func TestSettingsShowHostedAddressOnlyWhenOn(t *testing.T) {
	off, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, off, service, "Hosted Off", "admin@hosted-off.example.com")
	if address := hostedAddressOf(t, off, admin); address != nil {
		t.Fatalf("hosted address with the feature off: %q", *address)
	}
	on, service, _, _ := testAPIWithConfig(t, hostedConfig)
	admin = createSession(t, on, service, "Hosted On", "admin@hosted-on.example.com")
	if address := hostedAddressOf(t, on, admin); address == nil || *address != "support@hosted-on."+hostedDomain {
		t.Fatalf("hosted address = %v", address)
	}
	reserved := createSession(t, on, service, "Support", "admin@reserved-slug.example.com")
	if address := hostedAddressOf(t, on, reserved); address != nil {
		t.Fatalf("a reserved slug must have no hosted address, got %q", *address)
	}
}

func TestHostedAddressMailReachesOnlyItsOwnWorkspace(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, hostedConfig)
	adminA := createSession(t, api, service, "Hosted Alpha", "admin@hosted-alpha.example.com")
	adminB := createSession(t, api, service, "Hosted Beta", "admin@hosted-beta.example.com")
	connectEmail(t, api, adminA, "support@alpha.example.com")
	connectEmail(t, api, adminB, "support@beta.example.com")

	// The sender names workspace B in To and Cc and a lower X-Original-To; only the envelope line the
	// forwarder put on top decides.
	raw := hostedMail("support@hosted-alpha."+hostedDomain, "cross@customer.example",
		"To: support@hosted-beta."+hostedDomain, "Cc: support@hosted-beta."+hostedDomain, "X-Original-To: support@hosted-beta."+hostedDomain)
	result := inboundOK(t, api, platformSecret, raw)
	if result.Status != "created" || result.ConversationID == nil {
		t.Fatalf("result = %+v", result)
	}
	if got := conversationList(t, api, adminA); len(got) != 1 || got[0].ID != *result.ConversationID {
		t.Fatalf("workspace A conversations = %+v", got)
	}
	if got := conversationList(t, api, adminB); len(got) != 0 {
		t.Fatalf("mail leaked into workspace B: %+v", got)
	}
	if rows := queryString(t, pool, `SELECT count(*) FROM messages WHERE external_id='email:cross@customer.example'`); rows != "1" {
		t.Fatalf("stored %s copies", rows)
	}
}

func TestHostedAddressRejectsMailItCannotRoute(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, hostedConfig)
	admin := createSession(t, api, service, "Hosted Gamma", "admin@hosted-gamma.example.com")
	createSession(t, api, service, "Hosted Unconnected", "admin@hosted-unconnected.example.com")
	connectEmail(t, api, admin, "support@gamma.example.com")

	for name, envelope := range map[string]string{
		"unknown slug":                          "support@nobody." + hostedDomain,
		"workspace without a connected channel": "support@hosted-unconnected." + hostedDomain,
		"reserved slug":                         "support@app." + hostedDomain,
		"nested subdomain":                      "support@x.hosted-gamma." + hostedDomain,
		"apex":                                  "support@" + hostedDomain,
	} {
		response := inbound(t, api, platformSecret, hostedMail(envelope, "reject-"+strings.ReplaceAll(name, " ", "-")+"@customer.example"))
		if response.Code != http.StatusNotFound {
			t.Errorf("%s: status=%d body=%s, want 404", name, response.Code, response.Body.String())
		}
	}
	// Mail that names no hosted address is not ours to route: stored nowhere, reported as before.
	for name, raw := range map[string]string{
		"another domain":     hostedMail("support@hosted-gamma.elsewhere.example", "elsewhere@customer.example"),
		"no envelope header": mail([]string{"From: z@customer.example", "To: support@hosted-gamma." + hostedDomain, "Subject: x", "Message-ID: <no-envelope@customer.example>"}, "x"),
	} {
		if result := inboundOK(t, api, platformSecret, raw); result.Status != "dropped" || result.Reason == nil || *result.Reason != "unknown_recipient" {
			t.Errorf("%s: %+v", name, result)
		}
	}
	if response := inbound(t, api, platformSecret+"x", hostedMail("support@hosted-gamma."+hostedDomain, "wrong-secret@customer.example")); response.Code != http.StatusUnauthorized {
		t.Fatalf("wrong secret status=%d", response.Code)
	}
	if rows := queryString(t, pool, `SELECT count(*) FROM messages WHERE kind='incoming'`); rows != "0" {
		t.Fatalf("rejected mail stored %s messages", rows)
	}
	off, _, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Email.PlatformInboundSecret = platformSecret
		config.Email.MailFromDomain = "mail.colliedesk.test"
	})
	if response := inbound(t, off, platformSecret, hostedMail("support@hosted-gamma."+hostedDomain, "off@customer.example")); response.Code != http.StatusOK {
		t.Fatalf("feature off must keep the platform path unchanged, status=%d", response.Code)
	}
}

func TestWorkspaceSecretStillIgnoresHostedRouting(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, hostedConfig)
	adminA := createSession(t, api, service, "Hosted Delta", "admin@hosted-delta.example.com")
	adminB := createSession(t, api, service, "Hosted Epsilon", "admin@hosted-epsilon.example.com")
	secretA := connectEmail(t, api, adminA, "support@delta.example.com")
	connectEmail(t, api, adminB, "support@epsilon.example.com")
	result := inboundOK(t, api, secretA, hostedMail("support@hosted-epsilon."+hostedDomain, "own-secret@customer.example"))
	if result.Status != "created" || len(conversationList(t, api, adminA)) != 1 || len(conversationList(t, api, adminB)) != 0 {
		t.Fatalf("a workspace secret must file into its own workspace: %+v", result)
	}
}

const gmailConfirmation = `Delivered-To: support@hosted-gmail.desk.colliedesk.test
Received: by 2002:a05:6000:1 with SMTP id x; Thu, 15 Oct 2026 04:00:00 -0700 (PDT)
ARC-Seal: i=1; a=rsa-sha256; t=1760526000; cv=none; d=google.com; s=arc-20240605
ARC-Message-Signature: i=1; a=rsa-sha256; c=relaxed/relaxed; d=google.com; s=arc-20240605
ARC-Authentication-Results: i=1; mx.google.com; dkim=pass header.i=@accounts.google.com; spf=pass; dmarc=pass
Authentication-Results: mx.cloudflare.net; dkim=pass header.d=accounts.google.com; spf=pass smtp.mailfrom=forwarding-noreply@google.com; dmarc=pass
From: Gmail Team <forwarding-noreply@google.com>
To: support@hosted-gmail.desk.colliedesk.test
Subject: Gmail Forwarding Confirmation - Receive Mail from owner@acme-inc.example
Message-ID: <gmail-confirm-1@mail.gmail.com>
Content-Type: text/plain; charset="UTF-8"

owner@acme-inc.example has requested to automatically forward mail to support@hosted-gmail.desk.colliedesk.test.
To allow owner@acme-inc.example to automatically forward mail to your address, click the link below to confirm the request:

https://mail-settings.google.com/mail/vf-%5BANGjdJ%5D-synthetic-code

Confirmation code: 123456789
`

func TestGmailForwardingConfirmationBecomesAVisibleConversation(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		hostedConfig(config)
		config.Email.TrustedAuthServID = "mx.cloudflare.net"
	})
	admin := createSession(t, api, service, "Hosted Gmail", "admin@hosted-gmail.example.com")
	connectEmail(t, api, admin, "support@acme-inc.example")
	raw := "X-Original-To: support@hosted-gmail." + hostedDomain + "\r\n" + strings.ReplaceAll(gmailConfirmation, "\n", "\r\n")
	result := inboundOK(t, api, platformSecret, raw)
	if result.Status != "created" || result.ConversationID == nil {
		t.Fatalf("confirmation not stored: %+v", result)
	}
	items := timeline(t, api, admin, *result.ConversationID)
	if len(items) != 1 || items[0].Email == nil || items[0].Email.From != "forwarding-noreply@google.com" ||
		!strings.Contains(items[0].Body, "https://mail-settings.google.com/mail/vf-") {
		t.Fatalf("timeline = %+v", items)
	}
}

func TestForwardedCustomerMailKeepsTheOriginalSender(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		hostedConfig(config)
		config.Email.TrustedAuthServID = "mx.cloudflare.net"
	})
	admin := createSession(t, api, service, "Hosted Forward", "admin@hosted-forward.example.com")
	connectEmail(t, api, admin, "support@acme-inc.example")
	forwarded := func(id, results string) string {
		return hostedMail("support@hosted-forward."+hostedDomain, id,
			"Authentication-Results: mx.cloudflare.net; "+results,
			"ARC-Authentication-Results: i=1; mx.google.com; dkim=pass; dmarc=pass",
			"To: support@acme-inc.example", "Delivered-To: owner@acme-inc.example")
	}
	pass := inboundOK(t, api, platformSecret, forwarded("fwd-pass@customer.example", "dkim=pass header.d=customer.example; spf=softfail; dmarc=pass"))
	if pass.Status != "created" || pass.Authentication != "pass" {
		t.Fatalf("forwarded mail with a DKIM-aligned DMARC pass: %+v", pass)
	}
	forged := inboundOK(t, api, platformSecret, forwarded("fwd-fail@customer.example", "dkim=none; spf=fail; dmarc=fail"))
	if forged.Status != "created" || forged.Authentication != "fail" {
		t.Fatalf("a failing verdict is shown, not enforced: %+v", forged)
	}
	items := timeline(t, api, admin, *pass.ConversationID)
	if items[0].Email == nil || items[0].Email.From != "z@customer.example" {
		t.Fatalf("original sender lost: %+v", items[0].Email)
	}
}

func TestAdminChoosesTheWorkspaceMailSlug(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, hostedConfig)
	admin := createSession(t, api, service, "Hosted Zeta", "admin@hosted-zeta.example.com")
	other := createSession(t, api, service, "Hosted Eta", "admin@hosted-eta.example.com")
	connectEmail(t, api, admin, "support@zeta.example.com")

	patch := func(token, slug string) int {
		return request(t, api, http.MethodPatch, "/api/v1/workspace/slug", map[string]any{"slug": slug}, token, nil).Code
	}
	for name, slug := range map[string]string{"reserved": "app", "uppercase": "Zeta", "too long": strings.Repeat("a", 64), "dot": "a.b", "empty": ""} {
		if code := patch(admin, slug); code != http.StatusBadRequest {
			t.Errorf("%s slug: status=%d, want 400", name, code)
		}
	}
	if code := patch(admin, "hosted-eta"); code != http.StatusConflict {
		t.Errorf("taken slug: status=%d, want 409", code)
	}
	if code := patch(admin, "zeta-support"); code != http.StatusOK {
		t.Fatalf("valid slug: status=%d", code)
	}
	if address := hostedAddressOf(t, api, admin); address == nil || *address != "support@zeta-support."+hostedDomain {
		t.Fatalf("hosted address = %v", address)
	}
	if code := inbound(t, api, platformSecret, hostedMail("support@zeta-support."+hostedDomain, "new-slug@customer.example")).Code; code != http.StatusOK {
		t.Fatalf("new address status=%d", code)
	}
	if code := inbound(t, api, platformSecret, hostedMail("support@hosted-zeta."+hostedDomain, "old-slug@customer.example")).Code; code != http.StatusNotFound {
		t.Fatalf("old address status=%d, want 404", code)
	}
	if code := patch("", "no-session"); code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d", code)
	}
	if got := conversationList(t, api, other); len(got) != 0 {
		t.Fatalf("other workspace sees %+v", got)
	}
}

func TestSupportAddressCannotPointAtAnotherWorkspacesHostedAddress(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, hostedConfig)
	admin := createSession(t, api, service, "Hosted Theta", "admin@hosted-theta.example.com")
	createSession(t, api, service, "Hosted Iota", "admin@hosted-iota.example.com")
	put := func(address string) int {
		return request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": address}, admin, nil).Code
	}
	if code := put("support@hosted-iota." + hostedDomain); code != http.StatusBadRequest {
		t.Errorf("another workspace's hosted address: status=%d, want 400", code)
	}
	if code := put("support@hosted-theta." + hostedDomain); code != http.StatusOK {
		t.Errorf("own hosted address: status=%d, want 200", code)
	}
}

// The hosted domain itself is the operator's own support domain (it forwards into a workspace like any
// customer domain). Sending as it still needs domain verification, which only its DNS owner can pass.
func TestSupportAddressMayUseTheHostedDomainItself(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, hostedConfig)
	admin := createSession(t, api, service, "Hosted Kappa", "admin@hosted-kappa.example.com")
	response := request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "support@" + hostedDomain}, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("support address at the hosted domain: status=%d body=%s, want 200", response.Code, response.Body.String())
	}
}

const recipientPath = "/api/v1/channels/email/inbound/recipient"

func recipientCheck(t *testing.T, api http.Handler, secret, recipient string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, recipientPath, strings.NewReader(`{"recipient":"`+recipient+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("X-Colliedesk-Inbound-Secret", secret)
	}
	return serve(api, req).Code
}

// The MTA asks before it accepts RCPT TO, so an address nobody owns is refused in the SMTP session and
// never bounced afterwards to a sender that may be forged. The answer must match what inbound accepts.
func TestRecipientCheckAnswersLikeHostedInbound(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, hostedConfig)
	admin := createSession(t, api, service, "Hosted Zeta", "admin@hosted-zeta.example.com")
	createSession(t, api, service, "Hosted Eta", "admin@hosted-eta.example.com")
	secret := connectEmail(t, api, admin, "support@zeta.example.com")

	if code := recipientCheck(t, api, platformSecret, "Support@Hosted-Zeta."+hostedDomain); code != http.StatusNoContent {
		t.Fatalf("connected workspace: status=%d, want 204", code)
	}
	if code := recipientCheck(t, api, platformSecret, "billing+x@hosted-zeta."+hostedDomain); code != http.StatusNoContent {
		t.Fatalf("any local part of a connected workspace: status=%d, want 204", code)
	}
	for name, recipient := range map[string]string{
		"unknown slug":                          "support@nobody." + hostedDomain,
		"workspace without a connected channel": "support@hosted-eta." + hostedDomain,
		"reserved slug":                         "support@app." + hostedDomain,
		"nested subdomain":                      "support@x.hosted-zeta." + hostedDomain,
		"apex":                                  "support@" + hostedDomain,
		"another domain":                        "support@hosted-zeta.elsewhere.example",
		"not an address":                        "hosted-zeta." + hostedDomain,
	} {
		if code := recipientCheck(t, api, platformSecret, recipient); code != http.StatusNotFound {
			t.Errorf("%s: status=%d, want 404", name, code)
		}
	}
	for name, given := range map[string]string{"no secret": "", "wrong secret": platformSecret + "x", "workspace secret": secret} {
		if code := recipientCheck(t, api, given, "support@hosted-zeta."+hostedDomain); code != http.StatusUnauthorized {
			t.Errorf("%s: status=%d, want 401", name, code)
		}
	}
	off, _, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Email.PlatformInboundSecret = platformSecret })
	if code := recipientCheck(t, off, platformSecret, "support@hosted-zeta."+hostedDomain); code != http.StatusNotFound {
		t.Fatalf("hosted addresses off: status=%d, want 404", code)
	}
}
