package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

type emailSettings struct {
	Channel *struct {
		SupportAddress  string  `json:"support_address"`
		SecretSet       bool    `json:"secret_set"`
		SecretRotatedAt *string `json:"secret_rotated_at"`
	} `json:"channel"`
	InboundURL string `json:"inbound_url"`
	SMTP       struct {
		Configured bool `json:"configured"`
	} `json:"smtp"`
}

func getEmailSettings(t *testing.T, api http.Handler, token string) (emailSettings, string) {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/channels/email", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET email channel status=%d body=%s", response.Code, response.Body.String())
	}
	var settings emailSettings
	decode(t, response, &settings)
	return settings, response.Body.String()
}

func TestEmailChannelSettingsLifecycle(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Settings Mail", "admin@settings-mail.example.com")

	settings, _ := getEmailSettings(t, api, admin)
	if settings.Channel != nil || settings.InboundURL != testOrigin+inboundPath || !settings.SMTP.Configured {
		t.Fatalf("unexpected initial settings: %+v", settings)
	}
	if response := request(t, api, http.MethodPost, "/api/v1/channels/email/secret", nil, admin, nil); response.Code != http.StatusNotFound {
		t.Fatalf("rotate without channel status=%d, want 404", response.Code)
	}
	for _, invalid := range []string{"", "not-an-address", "Name <a@b.example>", "a@b.example\r\nBcc: x@y.example"} {
		if response := request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": invalid}, admin, nil); response.Code != http.StatusBadRequest {
			t.Errorf("support address %q status=%d, want 400", invalid, response.Code)
		}
	}

	if response := request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "Help@Settings-Mail.example.com"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", response.Code, response.Body.String())
	}
	settings, _ = getEmailSettings(t, api, admin)
	if settings.Channel == nil || settings.Channel.SupportAddress != "help@settings-mail.example.com" || settings.Channel.SecretSet {
		t.Fatalf("channel not saved: %+v", settings.Channel)
	}

	secret := rotateSecret(t, api, admin)
	settings, raw := getEmailSettings(t, api, admin)
	if !settings.Channel.SecretSet || settings.Channel.SecretRotatedAt == nil || !strings.HasSuffix(*settings.Channel.SecretRotatedAt, "Z") {
		t.Fatalf("secret state not visible: %+v", settings.Channel)
	}
	if strings.Contains(raw, secret) || strings.Contains(raw, secret[strings.Index(secret, ".")+1:]) {
		t.Fatal("settings response leaks the inbound secret")
	}

	if response := request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "support@settings-mail.example.com"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("update address status=%d", response.Code)
	}
	if inboundOK(t, api, secret, mail([]string{"From: a@customer.example", "Subject: x", "Message-ID: <keep@customer.example>"}, "x")).Status != "created" {
		t.Fatal("changing the address must keep the secret")
	}

	if response := request(t, api, http.MethodDelete, "/api/v1/channels/email", nil, admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("DELETE status=%d", response.Code)
	}
	if settings, _ = getEmailSettings(t, api, admin); settings.Channel != nil {
		t.Fatalf("channel still present after disconnect: %+v", settings.Channel)
	}
	if response := request(t, api, http.MethodDelete, "/api/v1/channels/email", nil, admin, nil); response.Code != http.StatusNotFound {
		t.Fatalf("second DELETE status=%d, want 404", response.Code)
	}
	if conversations := conversationList(t, api, admin); len(conversations) != 1 {
		t.Fatal("disconnecting must keep conversations")
	}
}

func TestEmailChannelSettingsStayWorkspaceScoped(t *testing.T) {
	api, service := testAPI(t)
	adminA := createSession(t, api, service, "Scoped Mail A", "admin@scoped-a.example.com")
	adminB := createSession(t, api, service, "Scoped Mail B", "admin@scoped-b.example.com")
	_ = connectEmail(t, api, adminB, "support@scoped-b.example.com")
	settings, raw := getEmailSettings(t, api, adminA)
	if settings.Channel != nil || strings.Contains(raw, "scoped-b") {
		t.Fatalf("workspace A sees workspace B channel: %s", raw)
	}
	if response := request(t, api, http.MethodDelete, "/api/v1/channels/email", nil, adminA, nil); response.Code != http.StatusNotFound {
		t.Fatalf("A deleting with no channel status=%d, want 404", response.Code)
	}
	if settings, _ := getEmailSettings(t, api, adminB); settings.Channel == nil {
		t.Fatal("B channel removed by A")
	}
}

func TestRepliesAreQueuedForEmailButNotesNever(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Queue Desk", "admin@queue.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Mo", "email": "mo@customer.example"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Queue"})
	_ = create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Before channel."})

	_ = connectEmail(t, api, admin, "support@queue.example.com")
	_ = create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "After channel."})
	_ = create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "note", "body": "Internal only."})

	items := timeline(t, api, admin, conversation)
	if len(items) != 3 {
		t.Fatalf("unexpected timeline: %+v", items)
	}
	// Written before the channel existed: not emailed by itself, and it says so (a Retry sends it).
	if items[0].Delivery == nil || items[0].Delivery.Status != "failed" {
		t.Fatalf("reply written before the channel existed must not be emailed, and must say it was not sent: %+v", items[0].Delivery)
	}
	if items[1].Delivery == nil || items[1].Delivery.Status != "queued" || items[1].Delivery.Attempts != 0 {
		t.Fatalf("reply must be queued: %+v", items[1])
	}
	if items[2].Delivery != nil {
		t.Fatalf("note must never be queued: %+v", items[2])
	}
	if count := queryString(t, pool, `SELECT count(*)::text FROM email_outbox WHERE status='queued'`); count != "1" {
		t.Fatalf("outbox rows=%s, want 1", count)
	}
	retry := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages/"+items[1].ID+"/retry", nil, admin, nil)
	if retry.Code != http.StatusConflict {
		t.Fatalf("retry of a queued delivery status=%d, want 409", retry.Code)
	}
	if note := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages/"+items[2].ID+"/retry", nil, admin, nil); note.Code != http.StatusNotFound {
		t.Fatalf("retry of a note status=%d, want 404", note.Code)
	}
}

func TestRetryDeliveryIsWorkspaceScoped(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	adminA := createSession(t, api, service, "Retry Alpha", "admin@retry-alpha.example.com")
	adminB := createSession(t, api, service, "Retry Beta", "admin@retry-beta.example.com")
	secretB := connectEmail(t, api, adminB, "support@retry-beta.example.com")
	thread := inboundOK(t, api, secretB, mail([]string{"From: ned@customer.example", "Subject: B", "Message-ID: <b@customer.example>"}, "B."))
	reply := create(t, api, adminB, "/api/v1/conversations/"+*thread.ConversationID+"/messages", map[string]any{"kind": "outgoing", "body": "B reply."})
	if _, err := pool.Exec(t.Context(), `UPDATE email_outbox SET status='failed', last_error='550 rejected'`); err != nil {
		t.Fatal(err)
	}
	response := request(t, api, http.MethodPost, "/api/v1/conversations/"+*thread.ConversationID+"/messages/"+reply+"/retry", nil, adminA, nil)
	if response.Code != http.StatusNotFound {
		t.Fatalf("cross-workspace retry status=%d, want 404", response.Code)
	}
	if status := queryString(t, pool, `SELECT status FROM email_outbox`); status != "failed" {
		t.Fatalf("foreign retry changed delivery to %s", status)
	}
	response = request(t, api, http.MethodPost, "/api/v1/conversations/"+*thread.ConversationID+"/messages/"+reply+"/retry", nil, adminB, nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"queued"`) {
		t.Fatalf("own retry status=%d body=%s", response.Code, response.Body.String())
	}
}
