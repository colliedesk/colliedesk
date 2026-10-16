package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// The hosted cloud sends replies from reply+<token>@MAIL_FROM_DOMAIN where MAIL_FROM_DOMAIN is itself
// below HOSTED_MAIL_DOMAIN. Such an address is a reply address, never a workspace slug.
const (
	cloudHostedDomain   = "colliedesk.test"
	cloudMailFromDomain = "mail.colliedesk.test"
)

func cloudReplyConfig(config *httpapi.Config) {
	config.Email.HostedMailDomain = cloudHostedDomain
	config.Email.MailFromDomain = cloudMailFromDomain
	config.Email.PlatformInboundSecret = platformSecret
}

func TestReplyTokenBelowTheHostedDomainThreadsIntoItsConversation(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, cloudReplyConfig)
	adminA := createSession(t, api, service, "Cloud Alpha", "admin@cloud-alpha.example.com")
	adminB := createSession(t, api, service, "Cloud Beta", "admin@cloud-beta.example.com")
	conversationA, _, tokenA := replyToken(t, api, pool, adminA, connectEmail(t, api, adminA, "support@alpha.example.com"), "ca@customer.example")
	conversationB, _, tokenB := replyToken(t, api, pool, adminB, connectEmail(t, api, adminB, "support@beta.example.com"), "cb@customer.example")

	for _, item := range []struct{ token, conversation, admin string }{{tokenA, conversationA, adminA}, {tokenB, conversationB, adminB}} {
		raw := hostedMail("reply+"+item.token+"@"+cloudMailFromDomain, "cloud-"+item.token+"@customer.example",
			"To: \"Desk via Colliedesk\" <reply+"+item.token+"@"+cloudMailFromDomain+">")
		result := inboundOK(t, api, platformSecret, raw)
		if result.Status != "threaded" || result.ConversationID == nil || *result.ConversationID != item.conversation {
			t.Fatalf("token %s: %+v, want threaded into %s", item.token, result, item.conversation)
		}
		if items := timeline(t, api, item.admin, item.conversation); len(items) != 3 {
			t.Fatalf("reply not in the owning workspace: %+v", items)
		}
	}
}

func TestReplyTokenOfAnotherWorkspaceNamedInToNeverRoutesThere(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, cloudReplyConfig)
	adminA := createSession(t, api, service, "Cloud Gamma", "admin@cloud-gamma.example.com")
	adminB := createSession(t, api, service, "Cloud Delta", "admin@cloud-delta.example.com")
	_, _, tokenA := replyToken(t, api, pool, adminA, connectEmail(t, api, adminA, "support@gamma.example.com"), "cg@customer.example")
	conversationB, _, _ := replyToken(t, api, pool, adminB, connectEmail(t, api, adminB, "support@delta.example.com"), "cd@customer.example")

	// The envelope names a token nobody owns; the sender-written To names workspace A's token.
	raw := hostedMail("reply+0123456789abcdef0123456789abcdef@"+cloudMailFromDomain, "forged@customer.example",
		"To: reply+"+tokenA+"@"+cloudMailFromDomain)
	result := inboundOK(t, api, platformSecret, raw)
	if result.Status != "dropped" || result.Reason == nil || *result.Reason != "unknown_recipient" {
		t.Fatalf("result = %+v, want dropped unknown_recipient", result)
	}
	if items := timeline(t, api, adminB, conversationB); len(items) != 2 {
		t.Fatalf("workspace B timeline changed: %+v", items)
	}
}

func TestReplyAddressCheckAnswersByTokenBeforeSlug(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, cloudReplyConfig)
	admin := createSession(t, api, service, "Cloud Epsilon", "admin@cloud-epsilon.example.com")
	_, _, token := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@epsilon.example.com"), "ce@customer.example")

	if code := recipientCheck(t, api, platformSecret, "reply+"+token+"@"+cloudMailFromDomain); code != http.StatusNoContent {
		t.Fatalf("known token: status=%d, want 204", code)
	}
	if code := recipientCheck(t, api, platformSecret, "Reply+"+token+"@Mail.Colliedesk.Test"); code != http.StatusNoContent {
		t.Fatalf("known token in upper case: status=%d, want 204", code)
	}
	for name, recipient := range map[string]string{
		"unknown token":        "reply+0123456789abcdef0123456789abcdef@" + cloudMailFromDomain,
		"no token":             "support@" + cloudMailFromDomain,
		"token on other host":  "reply+" + token + "@bounce." + cloudHostedDomain,
		"token at hosted apex": "reply+" + token + "@" + cloudHostedDomain,
	} {
		if code := recipientCheck(t, api, platformSecret, recipient); code != http.StatusNotFound {
			t.Errorf("%s: status=%d, want 404", name, code)
		}
	}
}

func TestSlugAddressesStillRouteWhenTheReplyDomainIsBelowTheHostedDomain(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, cloudReplyConfig)
	admin := createSession(t, api, service, "Cloud Zeta", "admin@cloud-zeta.example.com")
	connectEmail(t, api, admin, "support@zeta.example.com")

	if code := recipientCheck(t, api, platformSecret, "support@cloud-zeta."+cloudHostedDomain); code != http.StatusNoContent {
		t.Fatalf("slug address check: status=%d, want 204", code)
	}
	result := inboundOK(t, api, platformSecret, hostedMail("support@cloud-zeta."+cloudHostedDomain, "slug@customer.example"))
	if result.Status != "created" {
		t.Fatalf("slug address result = %+v", result)
	}
}

// Each reply is sent from reply+<token>-<reply id>@MAIL_FROM_DOMAIN; a customer answering that address
// reaches the conversation like one answering the older reply+<token>@ form.
func TestPerReplyAddressThreadsAndPassesTheRecipientCheck(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, cloudReplyConfig)
	admin := createSession(t, api, service, "Cloud Eta", "admin@cloud-eta.example.com")
	conversation, _, token := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@eta.example.com"), "ce2@customer.example")
	address := "reply+" + token + "-0123456789ab@" + cloudMailFromDomain

	if code := recipientCheck(t, api, platformSecret, address); code != http.StatusNoContent {
		t.Fatalf("per-reply address check: status=%d, want 204", code)
	}
	result := inboundOK(t, api, platformSecret, hostedMail(address, "per-reply@customer.example", "To: "+address))
	if result.Status != "threaded" || result.ConversationID == nil || *result.ConversationID != conversation {
		t.Fatalf("per-reply address: %+v, want threaded into %s", result, conversation)
	}
	for _, bad := range []string{"reply+" + token + "-0123@" + cloudMailFromDomain, "reply+" + token + "-0123456789abz@" + cloudMailFromDomain} {
		if code := recipientCheck(t, api, platformSecret, bad); code != http.StatusNotFound {
			t.Errorf("%s: status=%d, want 404", bad, code)
		}
	}
}
