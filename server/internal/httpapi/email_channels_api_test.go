package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/colliedesk/colliedesk/server/internal/microsoft"
	"github.com/colliedesk/colliedesk/server/internal/tokenseal"
)

type emailChannelView struct {
	Channel *struct {
		ChannelID        string  `json:"channel_id"`
		SupportAddress   string  `json:"support_address"`
		DomainVerifiedAt *string `json:"domain_verified_at"`
		DNSRecordName    string  `json:"dns_record_name"`
		DNSRecordValue   string  `json:"dns_record_value"`
	} `json:"channel"`
	HostedAddress *string `json:"hosted_address"`
}

func emailChannelOf(t *testing.T, api http.Handler, admin, channelID string) emailChannelView {
	t.Helper()
	path := "/api/v1/channels/email"
	if channelID != "" {
		path = "/api/v1/channels/" + channelID + "/email"
	}
	response := request(t, api, http.MethodGet, path, nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, http.MethodGet, path, nil, admin, nil), response)
	var view emailChannelView
	decode(t, response, &view)
	return view
}

// addEmailChannel adds a mailbox through the channels route and returns its id.
func addEmailChannel(t *testing.T, api http.Handler, admin, address string) string {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "email", "config": map[string]any{"support_address": address}}, admin, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("add %s status=%d body=%s", address, response.Code, response.Body.String())
	}
	var created channelBody
	decode(t, response, &created)
	return created.ID
}

// Two mailboxes on one domain verify it once: the TXT record is the same for both, and verifying
// through either verifies both. The domain stays while one of them uses it.
func TestTwoMailboxesOnOneDomainVerifyItOnce(t *testing.T) {
	api, service, pool, dns := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Shared Domain", "admin@shared-domain.example.com")
	connectEmail(t, api, admin, "support@shared-domain.example.com")
	billing := addEmailChannel(t, api, admin, "billing@shared-domain.example.com")
	first, second := emailChannelOf(t, api, admin, ""), emailChannelOf(t, api, admin, billing)
	if first.Channel.DNSRecordValue != second.Channel.DNSRecordValue || first.Channel.DNSRecordName != second.Channel.DNSRecordName {
		t.Fatalf("one domain asks two records: %+v and %+v", first.Channel, second.Channel)
	}
	dns.set(second.Channel.DNSRecordName, second.Channel.DNSRecordValue)
	if response := request(t, api, http.MethodPost, "/api/v1/channels/"+billing+"/email/domain-verification", nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("verify through the second mailbox: %d %s", response.Code, response.Body.String())
	}
	if view := emailChannelOf(t, api, admin, ""); view.Channel.DomainVerifiedAt == nil {
		t.Fatal("the first mailbox is not verified after the second verified the domain")
	}
	if response := request(t, api, http.MethodDelete, "/api/v1/channels/"+first.Channel.ChannelID, nil, admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("remove the first mailbox: %d %s", response.Code, response.Body.String())
	}
	if view := emailChannelOf(t, api, admin, billing); view.Channel.DomainVerifiedAt == nil {
		t.Fatal("removing one mailbox lost the domain's verification")
	}
	if domains := queryString(t, pool, `SELECT count(*)::text FROM email_domains`); domains != "1" {
		t.Fatalf("email_domains rows = %s, want 1", domains)
	}
	if response := request(t, api, http.MethodDelete, "/api/v1/channels/"+billing, nil, admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("remove the second mailbox: %d %s", response.Code, response.Body.String())
	}
	if domains := queryString(t, pool, `SELECT count(*)::text FROM email_domains`); domains != "0" {
		t.Fatalf("email_domains rows = %s after the last mailbox went, want 0", domains)
	}
}

// A support address names one mailbox of the workspace: an address another channel has, with a
// +tag or another case, is refused with the channel that has it, at every route that sets one.
func TestASupportAddressBelongsToOneEmailChannel(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "One Mailbox", "admin@one-mailbox.example.com")
	connectEmail(t, api, admin, "support@one-mailbox.example.com")
	first := emailChannelOf(t, api, admin, "").Channel.ChannelID
	check := func(method, path string, body map[string]any) {
		t.Helper()
		response := request(t, api, method, path, body, admin, nil)
		if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"code":"support_address_taken"`) ||
			!strings.Contains(response.Body.String(), `"channel_id":"`+first+`"`) || !strings.Contains(response.Body.String(), `"support_address"`) {
			t.Fatalf("%s %s: %d %s, want 409 support_address_taken naming %s", method, path, response.Code, response.Body.String(), first)
		}
		validateContractResponse(t, newRequest(t, method, path, body, admin, nil), response)
	}
	check(http.MethodPost, "/api/v1/channels", map[string]any{"type": "email", "config": map[string]any{"support_address": "Support+VIP@one-mailbox.example.com"}})
	billing := addEmailChannel(t, api, admin, "billing@one-mailbox.example.com")
	check(http.MethodPatch, "/api/v1/channels/"+billing, map[string]any{"config": map[string]any{"support_address": "support@one-mailbox.example.com"}})
	if response := request(t, api, http.MethodPatch, "/api/v1/channels/"+billing, map[string]any{"config": map[string]any{"support_address": "billing+desk@one-mailbox.example.com"}}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("a mailbox's own +tag address: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "email", "config": map[string]any{"support_address": "+x@one-mailbox.example.com"}}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("an address whose local part starts with +: %d %s, want 400", response.Code, response.Body.String())
	}
	// A removed channel's address is free again.
	if response := request(t, api, http.MethodDelete, "/api/v1/channels/"+first, nil, admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("remove: %d", response.Code)
	}
	addEmailChannel(t, api, admin, "support@one-mailbox.example.com")
}

// On the hosted cloud the first mailbox forwards to support@, the next ones to their own local part,
// numbered when two share one; mail to each reaches its channel, and another local part the default.
func TestEachMailboxHasItsOwnForwardingAddress(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, hostedConfig)
	admin := createSession(t, api, service, "Many Boxes", "admin@many-boxes.example.com")
	connectEmail(t, api, admin, "help@many-boxes.example.com")
	billing := addEmailChannel(t, api, admin, "billing@many-boxes.example.com")
	again := addEmailChannel(t, api, admin, "billing@elsewhere.example.com")
	for _, item := range []struct{ channel, want string }{{"", "support@many-boxes." + hostedDomain}, {billing, "billing@many-boxes." + hostedDomain},
		{again, "billing-2@many-boxes." + hostedDomain}} {
		if got := emailChannelOf(t, api, admin, item.channel).HostedAddress; got == nil || *got != item.want {
			t.Errorf("channel %q forwards to %v, want %s", item.channel, got, item.want)
		}
	}
	deliver := func(envelope, messageID string) string {
		t.Helper()
		response := serve(api, inboundRequest(t, hostedMail(envelope, messageID), platformSecret))
		var result struct {
			ConversationID *string `json:"conversation_id"`
		}
		decode(t, response, &result)
		if response.Code != http.StatusOK || result.ConversationID == nil {
			t.Fatalf("deliver to %s: %d %s", envelope, response.Code, response.Body.String())
		}
		var conversation struct {
			ChannelRef *struct {
				ID string `json:"id"`
			} `json:"channel_ref"`
		}
		decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+*result.ConversationID, nil, admin, nil), &conversation)
		return conversation.ChannelRef.ID
	}
	defaultID := emailChannelOf(t, api, admin, "").Channel.ChannelID
	if got := deliver("billing@many-boxes."+hostedDomain, "to-billing@customer.example"); got != billing {
		t.Errorf("mail to billing@ arrived on %s, want %s", got, billing)
	}
	// A +tag names the same mailbox, as it does for support addresses.
	if got := deliver("Billing+invoice@many-boxes."+hostedDomain, "to-billing-tag@customer.example"); got != billing {
		t.Errorf("mail to billing+invoice@ arrived on %s, want %s", got, billing)
	}
	if got := deliver("anything@many-boxes."+hostedDomain, "to-anything@customer.example"); got != defaultID {
		t.Errorf("mail to another local part arrived on %s, want the default %s", got, defaultID)
	}
}

// Self-hosted, each mailbox has its inbound secret: mail forwarded with one arrives on its channel.
func TestEachMailboxSecretBringsMailToItsChannel(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Secret Boxes", "admin@secret-boxes.example.com")
	connectEmail(t, api, admin, "support@secret-boxes.example.com")
	billing := addEmailChannel(t, api, admin, "billing@secret-boxes.example.com")
	response := request(t, api, http.MethodPost, "/api/v1/channels/"+billing+"/email/secret", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("rotate the second secret: %d %s", response.Code, response.Body.String())
	}
	var rotated struct {
		Secret string `json:"inbound_secret"`
	}
	decode(t, response, &rotated)
	delivered := serve(api, inboundRequest(t, mail([]string{"From: z@customer.example", "To: billing@secret-boxes.example.com", "Subject: Invoice",
		"Message-ID: <secret-billing@customer.example>"}, "Wrong amount."), rotated.Secret))
	var result struct {
		ConversationID *string `json:"conversation_id"`
	}
	decode(t, delivered, &result)
	if delivered.Code != http.StatusOK || result.ConversationID == nil {
		t.Fatalf("deliver: %d %s", delivered.Code, delivered.Body.String())
	}
	var conversation struct {
		ChannelRef struct {
			ID string `json:"id"`
		} `json:"channel_ref"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+*result.ConversationID, nil, admin, nil), &conversation)
	if conversation.ChannelRef.ID != billing {
		t.Fatalf("mail with the second secret arrived on %s, want %s", conversation.ChannelRef.ID, billing)
	}
}

// The routes of one email channel of several answer only for this workspace's email channels.
func TestEmailChannelRoutesStayInTheirWorkspace(t *testing.T) {
	keys, err := tokenseal.ParseKeys("k1:AgICAgICAgICAgICAgICAgICAgICAgICAgICAgICAgI=")
	if err != nil {
		t.Fatal(err)
	}
	// Microsoft 365 is set up, so its routes reach the channel lookup instead of answering 409 first.
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		config.Microsoft = microsoft.Config{ClientID: "ms-client", ClientSecret: "ms-secret", Tenant: "common", Keys: keys, Base: "http://127.0.0.1:1"}
	})
	alpha := createSession(t, api, service, "Alpha Boxes", "admin@alpha-boxes.example.com")
	beta := createSession(t, api, service, "Beta Boxes", "admin@beta-boxes.example.com")
	connectEmail(t, api, alpha, "support@alpha-boxes.example.com")
	connectEmail(t, api, beta, "support@beta-boxes.example.com")
	betaChannel := emailChannelOf(t, api, beta, "").Channel.ChannelID
	request(t, api, http.MethodPut, "/api/v1/channels/web-form", map[string]any{"enabled": true, "allowed_origins": []string{}}, alpha, nil)
	var forms struct {
		Items []channelBody `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels", nil, alpha, nil), &forms)
	formID := ""
	for _, item := range forms.Items {
		if item.Type == "web_form" {
			formID = item.ID
		}
	}
	for _, id := range []string{betaChannel, formID, "00000000-0000-4000-8000-000000000000", "not-an-id"} {
		for _, call := range []struct{ method, path string }{{http.MethodGet, "/email"}, {http.MethodPost, "/email/secret"},
			{http.MethodPost, "/email/domain-verification"}, {http.MethodDelete, "/email/gmail"},
			{http.MethodPost, "/email/microsoft"}, {http.MethodDelete, "/email/microsoft"}} {
			response := request(t, api, call.method, "/api/v1/channels/"+id+call.path, nil, alpha, nil)
			if response.Code != http.StatusNotFound || strings.Contains(response.Body.String(), "beta-boxes") {
				t.Errorf("%s %s of %s: %d %s, want 404", call.method, call.path, id, response.Code, response.Body.String())
			}
		}
	}
	if view := emailChannelOf(t, api, beta, ""); view.Channel.ChannelID != betaChannel {
		t.Fatal("beta's channel changed")
	}
}

// A request an agent files about one mailbox names it, so the person approving knows which one.
func TestARequestAboutAMailboxNamesIt(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Named Requests", "admin@named-requests.example.com")
	connectEmail(t, api, admin, "support@named-requests.example.com")
	billing := addEmailChannel(t, api, admin, "billing@named-requests.example.com")
	if response := request(t, api, http.MethodPatch, "/api/v1/channels/"+billing, map[string]any{"name": "Billing"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", response.Code, response.Body.String())
	}
	token := issueToken(t, api, admin, "Ops agent", "admin").Token
	for _, item := range []struct {
		method, path string
		body         any
		want         string
	}{
		{http.MethodDelete, "/api/v1/channels/" + billing, nil, "Remove the channel Billing (billing@named-requests.example.com)"},
		{http.MethodPost, "/api/v1/channels/" + billing + "/held/discard", nil, "Discard the held messages of the channel Billing (billing@named-requests.example.com)"},
		{http.MethodPost, "/api/v1/channels/" + billing + "/email/secret", nil, "Rotate the inbound secret of the channel Billing (billing@named-requests.example.com)"},
		{http.MethodPost, "/api/v1/channels/" + billing + "/email/gmail", nil, "Connect Gmail to the channel Billing (billing@named-requests.example.com)"},
		{http.MethodDelete, "/api/v1/channels/" + billing + "/email/gmail", nil, "Disconnect Gmail from the channel Billing (billing@named-requests.example.com)"},
	} {
		created := accepted(t, api, item.method, item.path, item.body, token)
		if !strings.HasPrefix(created.Summary, item.want) {
			t.Errorf("%s %s: summary %q, want it to start with %q", item.method, item.path, created.Summary, item.want)
		}
		if strings.Contains(created.Summary, "secret-value") {
			t.Errorf("%s %s: the summary shows the secret: %q", item.method, item.path, created.Summary)
		}
	}
}

// Mail from another mailbox of the workspace is the workspace's own, as mail from the receiving one is:
// it opens no conversation with the team as the customer.
func TestMailFromAnotherMailboxOfTheWorkspaceIsItsOwn(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Own Boxes", "admin@own-boxes.example.com")
	secret := connectEmail(t, api, admin, "support@own-boxes.example.com")
	addEmailChannel(t, api, admin, "billing@own-boxes.example.com")
	result := inboundOK(t, api, secret, mail([]string{"From: Billing <Billing+desk@own-boxes.example.com>", "To: support@own-boxes.example.com",
		"Subject: Internal", "Message-ID: <internal@own-boxes.example.com>"}, "From the billing mailbox."))
	if result.Status != "dropped" || result.Reason == nil || *result.Reason != "own_address" || result.ConversationID != nil {
		t.Fatalf("mail from billing@ to support@: %+v, want dropped/own_address", result)
	}
	// A customer still writes in.
	if result := inboundOK(t, api, secret, mail([]string{"From: olive@customer.example", "To: support@own-boxes.example.com",
		"Subject: Help", "Message-ID: <help@customer.example>"}, "Help.")); result.Status != "created" {
		t.Fatalf("customer mail: %+v, want created", result)
	}
}

// A channel's secret never waits in an agent request: a token below the admin tier is refused, as for
// an AI provider's key, and an admin token stores it at once. The value is in no request, no listing
// of requests and no export.
func TestAChannelSecretNeverWaitsInARequest(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Sealed Secrets", "admin@sealed-secrets.example.com")
	form := addWebForm(t, api, admin, "Contact form", []string{"https://shop.example.com"})
	const value = "sk-channel-probe-9z8y7x"
	body := map[string]any{"name": "access_token", "value": value}
	send := issueToken(t, api, admin, "Sender", "send").Token
	if response := request(t, api, http.MethodPut, "/api/v1/channels/"+form.ID+"/credentials", body, send, nil); response.Code != http.StatusForbidden ||
		!strings.Contains(response.Body.String(), "credentials_need_admin_token") {
		t.Fatalf("a send token: %d %s, want 403 credentials_need_admin_token", response.Code, response.Body.String())
	}
	adminToken := issueToken(t, api, admin, "Ops", "admin").Token
	if response := request(t, api, http.MethodPut, "/api/v1/channels/"+form.ID+"/credentials", body, adminToken, nil); response.Code == http.StatusAccepted {
		t.Fatalf("an admin token filed a request: %s", response.Body.String())
	}
	if stored := queryString(t, pool, `SELECT count(*)::text FROM agent_requests WHERE body::text LIKE '%'||$1||'%' OR summary LIKE '%'||$1||'%'`, value); stored != "0" {
		t.Fatalf("%s agent requests keep the secret", stored)
	}
	for _, token := range []string{admin, adminToken} {
		if listed := request(t, api, http.MethodGet, "/api/v1/agent-requests", nil, token, nil); strings.Contains(listed.Body.String(), value) {
			t.Fatalf("the requests list shows the secret: %s", listed.Body.String())
		}
	}
}
