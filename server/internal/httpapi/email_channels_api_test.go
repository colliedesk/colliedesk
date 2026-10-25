package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
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
	api, service, _, _ := testAPIWithConfig(t, nil)
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
			{http.MethodPost, "/email/domain-verification"}, {http.MethodDelete, "/email/gmail"}} {
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
