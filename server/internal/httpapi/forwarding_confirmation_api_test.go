package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

type forwardingConfirmationView struct {
	Code           string  `json:"code"`
	ConfirmURL     *string `json:"confirm_url"`
	ReceivedAt     string  `json:"received_at"`
	ConversationID string  `json:"conversation_id"`
}

// googleAuth is what the trusted receiving server stamps on mail Google really sent.
const googleAuth = "Authentication-Results: mx.example.net; spf=pass smtp.mailfrom=google.com; dkim=pass header.d=google.com; dmarc=pass header.from=google.com"

// syntheticGmailConfirmation is a SYNTHETIC Gmail "Forwarding Confirmation" email (written from Google's documented
// format, not a received one) sent to the support address; extra headers go first.
func syntheticGmailConfirmation(to, messageID string, extra ...string) string {
	headers := append(extra, "From: Gmail Team <forwarding-noreply@google.com>", "To: "+to,
		"Subject: (#482913557) Gmail Forwarding Confirmation - Receive Mail from "+to, "Message-ID: <"+messageID+"@google.com>")
	return mail(headers, "Confirmation code: 482913557\n\nTo allow it, please click the link below to confirm the request:\n\n"+
		"https://mail-settings.google.com/mail/vf-%5BSYNTHETIC%5D-abc\n\nTo cancel: https://mail-settings.google.com/mail/uf-%5BSYNTHETIC%5D-abc\n")
}

func forwardingConfirmation(t *testing.T, api http.Handler, token, channelID string) (int, forwardingConfirmationView) {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/channels/"+channelID+"/email/forwarding-confirmation", nil, token, nil)
	var view forwardingConfirmationView
	if response.Code == http.StatusOK {
		decode(t, response, &view)
	}
	return response.Code, view
}

func TestForwardingConfirmationShowsGmailsCodeAndConfirmLinkAndKeepsItsConversation(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Forward Desk", "admin@forward.example.com")
	secret := connectEmail(t, api, admin, "support@forward.example.com")
	channelID := queryString(t, pool, `SELECT id::text FROM channels WHERE type='email' AND workspace_id=(SELECT workspace_id FROM agents WHERE email='admin@forward.example.com')`)
	if code, _ := forwardingConfirmation(t, api, admin, channelID); code != http.StatusNotFound {
		t.Fatalf("before any confirmation: status=%d, want 404", code)
	}
	result := inboundOK(t, api, secret, syntheticGmailConfirmation("support@forward.example.com", "confirm-1", googleAuth))
	if result.Status != "created" || result.ConversationID == nil {
		t.Fatalf("the confirmation must still open its conversation: %+v", result)
	}
	code, got := forwardingConfirmation(t, api, admin, channelID)
	if code != http.StatusOK {
		t.Fatalf("status=%d, want 200", code)
	}
	if got.Code != "482913557" || got.ConfirmURL == nil || *got.ConfirmURL != "https://mail-settings.google.com/mail/vf-%5BSYNTHETIC%5D-abc" ||
		got.ConversationID != *result.ConversationID || got.ReceivedAt == "" || !strings.HasSuffix(got.ReceivedAt, "Z") {
		t.Fatalf("confirmation = %+v", got)
	}
}

// Anyone can mail the forwarding address claiming to be Google: without the trusted sender check the
// code shows, never a link.
func TestForwardingConfirmationWithoutAPassingSenderCheckHasNoLink(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Forged Desk", "admin@forged.example.com")
	secret := connectEmail(t, api, admin, "support@forged.example.com")
	channelID := queryString(t, pool, `SELECT id::text FROM channels WHERE type='email' AND workspace_id=(SELECT workspace_id FROM agents WHERE email='admin@forged.example.com')`)
	inboundOK(t, api, secret, syntheticGmailConfirmation("support@forged.example.com", "forged-1",
		"Authentication-Results: mx.example.net; spf=fail smtp.mailfrom=google.com; dmarc=fail header.from=google.com"))
	code, got := forwardingConfirmation(t, api, admin, channelID)
	if code != http.StatusOK || got.Code != "482913557" || got.ConfirmURL != nil {
		t.Fatalf("status=%d confirmation=%+v, want the code without a link", code, got)
	}
}

func TestForwardingConfirmationOlderThanADayIsGone(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Old Desk", "admin@old.example.com")
	secret := connectEmail(t, api, admin, "support@old.example.com")
	channelID := queryString(t, pool, `SELECT id::text FROM channels WHERE type='email' AND workspace_id=(SELECT workspace_id FROM agents WHERE email='admin@old.example.com')`)
	result := inboundOK(t, api, secret, syntheticGmailConfirmation("support@old.example.com", "old-1", googleAuth))
	if _, err := pool.Exec(t.Context(), `UPDATE messages SET created_at=now()-interval '25 hours' WHERE id=$1`, *result.MessageID); err != nil {
		t.Fatal(err)
	}
	if code, _ := forwardingConfirmation(t, api, admin, channelID); code != http.StatusNotFound {
		t.Fatalf("a confirmation from yesterday: status=%d, want 404", code)
	}
}

// Workspace isolation: another workspace's channel id answers 404, and its own channel never shows
// a confirmation that reached another workspace.
func TestForwardingConfirmationNeverCrossesWorkspaces(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	adminA := createSession(t, api, service, "Alpha Forward", "admin@alpha-forward.example.com")
	adminB := createSession(t, api, service, "Beta Forward", "admin@beta-forward.example.com")
	secretA := connectEmail(t, api, adminA, "support@alpha-forward.example.com")
	connectEmail(t, api, adminB, "support@beta-forward.example.com")
	channelOf := func(email string) string {
		return queryString(t, pool, `SELECT id::text FROM channels WHERE type='email' AND workspace_id=(SELECT workspace_id FROM agents WHERE email=$1)`, email)
	}
	channelA, channelB := channelOf("admin@alpha-forward.example.com"), channelOf("admin@beta-forward.example.com")
	inboundOK(t, api, secretA, syntheticGmailConfirmation("support@alpha-forward.example.com", "alpha-1", googleAuth))
	if code, _ := forwardingConfirmation(t, api, adminA, channelA); code != http.StatusOK {
		t.Fatalf("own confirmation: status=%d, want 200", code)
	}
	if code, _ := forwardingConfirmation(t, api, adminB, channelA); code != http.StatusNotFound {
		t.Fatalf("another workspace's channel: status=%d, want 404", code)
	}
	if code, _ := forwardingConfirmation(t, api, adminB, channelB); code != http.StatusNotFound {
		t.Fatalf("own channel of the other workspace: status=%d, want 404", code)
	}
}
