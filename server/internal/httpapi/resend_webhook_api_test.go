package httpapi_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	resendWebhookPath = "/api/v1/webhooks/resend"
	// A Svix secret: "whsec_" plus the base64 of the key below.
	resendWebhookSecret = "whsec_cmVzZW5kLXRlc3Qtc2lnbmluZy1rZXktMzJieXRlcw==" // gitleaks:allow (test placeholder)
	resendWebhookKey    = "resend-test-signing-key-32bytes"
)

func resendConfig(config *httpapi.Config) {
	platformConfig(config)
	config.Email.ResendWebhookSecret = resendWebhookSecret
}

// resendEvent is a Resend email event as documented for email.bounced and email.complained.
func resendEvent(kind, from string, to []string, messageID string, createdAt time.Time) []byte {
	data := map[string]any{
		"email_id": "56761188-7520-42d8-8898-ff6fc54ce618", "message_id": messageID, "from": from, "to": to,
		"subject": "Re: Platform", "created_at": createdAt.UTC().Format(time.RFC3339Nano),
	}
	if kind == "email.bounced" {
		data["bounce"] = map[string]any{"type": "Permanent", "subType": "General", "message": "Mailbox does not exist."}
	}
	body, _ := json.Marshal(map[string]any{"type": kind, "created_at": time.Now().UTC().Format(time.RFC3339Nano), "data": data})
	return body
}

func svixRequest(id string, timestamp int64, key string, body []byte) *http.Request {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(id + "." + strconv.FormatInt(timestamp, 10) + "."))
	mac.Write(body)
	req := httptest.NewRequest(http.MethodPost, resendWebhookPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("svix-id", id)
	req.Header.Set("svix-timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("svix-signature", "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return req
}

func signedResendRequest(id string, body []byte) *http.Request {
	return svixRequest(id, time.Now().Unix(), resendWebhookKey, body)
}

func deliverResend(t *testing.T, api http.Handler, id string, body []byte) string {
	t.Helper()
	response := serve(api, signedResendRequest(id, body))
	if response.Code != http.StatusOK {
		t.Fatalf("resend webhook status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Status string `json:"status"`
	}
	decode(t, response, &result)
	return result.Status
}

// sentReply makes the queued reply look delivered by the relay at the given time.
func sentReply(t *testing.T, pool *pgxpool.Pool, reply string, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), `UPDATE email_outbox SET status='sent',sent_at=$2,attempts=1 WHERE message_id=$1`, reply, at); err != nil {
		t.Fatal(err)
	}
}

func deliveryOf(t *testing.T, pool *pgxpool.Pool, reply string) (string, string) {
	t.Helper()
	return queryString(t, pool, `SELECT status FROM email_outbox WHERE message_id=$1`, reply),
		queryString(t, pool, `SELECT COALESCE(last_error,'') FROM email_outbox WHERE message_id=$1`, reply)
}

func platformFrom(token string) string {
	return "Desk via Colliedesk <reply+" + token + "@mail.colliedesk.test>"
}

func TestResendBounceMarksTheReplyDeliveryFailed(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, resendConfig)
	admin := createSession(t, api, service, "Resend Bounce", "admin@resend-bounce.example.com")
	conversation, reply, token := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@resend-bounce.example.com"), "rb@customer.example")
	sentReply(t, pool, reply, time.Now())

	// The relay rewrites the Message-ID, so the event names one Colliedesk never stored.
	body := resendEvent("email.bounced", platformFrom(token), []string{"Z@Customer.Example"}, "<0102-rewritten@eu-west-1.amazonses.com>", time.Now())
	if status := deliverResend(t, api, "msg_bounce_1", body); status != "applied" {
		t.Fatalf("status=%q, want applied", status)
	}
	items := timeline(t, api, admin, conversation)
	if items[1].Delivery == nil || items[1].Delivery.Status != "failed" {
		t.Fatalf("bounced reply delivery: %+v", items[1].Delivery)
	}
	if _, lastError := deliveryOf(t, pool, reply); !strings.Contains(lastError, "bounced") {
		t.Fatalf("bounce reason not recorded: %q", lastError)
	}
	if status := deliverResend(t, api, "msg_bounce_1", body); status != "ignored" {
		t.Fatalf("redelivery status=%q, want ignored", status)
	}
}

func TestResendComplaintMarksTheReplyDeliveryFailed(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, resendConfig)
	admin := createSession(t, api, service, "Resend Complaint", "admin@resend-complaint.example.com")
	_, reply, token := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@resend-complaint.example.com"), "rc@customer.example")
	sentReply(t, pool, reply, time.Now())

	body := resendEvent("email.complained", platformFrom(token), []string{"z@customer.example"}, "", time.Now())
	if status := deliverResend(t, api, "msg_complaint_1", body); status != "applied" {
		t.Fatalf("status=%q, want applied", status)
	}
	if status, lastError := deliveryOf(t, pool, reply); status != "failed" || !strings.Contains(lastError, "spam") {
		t.Fatalf("complaint: status=%q reason=%q", status, lastError)
	}
}

func TestResendBounceFailsTheReplyItWasSentFor(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, resendConfig)
	admin := createSession(t, api, service, "Resend Order", "admin@resend-order.example.com")
	conversation, first, token := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@resend-order.example.com"), "ro@customer.example")
	second := create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Second answer."})
	sentAt := time.Now().Add(-2 * time.Hour)
	sentReply(t, pool, first, sentAt)
	sentReply(t, pool, second, time.Now())

	// The event was created when the first reply was handed over, before the second existed.
	body := resendEvent("email.bounced", platformFrom(token), []string{"z@customer.example"}, "", sentAt.Add(-time.Second))
	if status := deliverResend(t, api, "msg_order_1", body); status != "applied" {
		t.Fatalf("status=%q, want applied", status)
	}
	if status, _ := deliveryOf(t, pool, first); status != "failed" {
		t.Fatalf("first reply status=%q, want failed", status)
	}
	if status, _ := deliveryOf(t, pool, second); status != "sent" {
		t.Fatalf("second reply status=%q, want sent", status)
	}

	// A relay that keeps our Message-ID names the reply exactly, even when a later one exists.
	if _, err := pool.Exec(t.Context(), `UPDATE email_outbox SET status='sent',last_error=NULL WHERE message_id=$1`, first); err != nil {
		t.Fatal(err)
	}
	ourID := queryString(t, pool, `SELECT email_message_id FROM messages WHERE id=$1`, first)
	exact := resendEvent("email.bounced", platformFrom(token), []string{"z@customer.example"}, "<"+ourID+">", time.Now())
	if status := deliverResend(t, api, "msg_order_2", exact); status != "applied" {
		t.Fatalf("status=%q, want applied", status)
	}
	if status, _ := deliveryOf(t, pool, first); status != "failed" {
		t.Fatalf("first reply by Message-ID status=%q, want failed", status)
	}
	if status, _ := deliveryOf(t, pool, second); status != "sent" {
		t.Fatalf("second reply status=%q after the exact match, want sent", status)
	}
}

func TestResendEventsThatNameNoReplyOfOursChangeNothing(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, resendConfig)
	adminA := createSession(t, api, service, "Resend Alpha", "admin@resend-alpha.example.com")
	adminB := createSession(t, api, service, "Resend Beta", "admin@resend-beta.example.com")
	_, replyA, tokenA := replyToken(t, api, pool, adminA, connectEmail(t, api, adminA, "support@resend-alpha.example.com"), "ra@customer.example")
	conversationB, replyB, _ := replyToken(t, api, pool, adminB, connectEmail(t, api, adminB, "support@resend-beta.example.com"), "rbeta@customer.example")
	sentReply(t, pool, replyA, time.Now())
	sentReply(t, pool, replyB, time.Now())
	ourIDB := queryString(t, pool, `SELECT email_message_id FROM messages WHERE id=$1`, replyB)

	for name, body := range map[string][]byte{
		"recipient we never sent to": resendEvent("email.bounced", platformFrom(tokenA), []string{"cc@customer.example"}, "", time.Now()),
		"unknown token":              resendEvent("email.bounced", platformFrom("0123456789abcdef0123456789abcdef"), []string{"z@customer.example"}, "", time.Now()),
		"verified support address":   resendEvent("email.bounced", "Desk <support@resend-alpha.example.com>", []string{"z@customer.example"}, "", time.Now()),
		"delivered event":            resendEvent("email.delivered", platformFrom(tokenA), []string{"z@customer.example"}, "", time.Now()),
	} {
		if status := deliverResend(t, api, "msg_"+strings.ReplaceAll(name, " ", "_"), body); status != "ignored" {
			t.Errorf("%s: status=%q, want ignored", name, status)
		}
	}
	for _, reply := range []string{replyA, replyB} {
		if status, _ := deliveryOf(t, pool, reply); status != "sent" {
			t.Fatalf("reply %s status=%q, want sent", reply, status)
		}
	}
	// Workspace A's token with workspace B's Message-ID is a bounce in A; it never reaches B.
	deliverResend(t, api, "msg_cross", resendEvent("email.bounced", platformFrom(tokenA), []string{"z@customer.example"}, "<"+ourIDB+">", time.Now()))
	if status, _ := deliveryOf(t, pool, replyB); status != "sent" {
		t.Fatalf("workspace B reply status=%q, want sent", status)
	}
	if items := timeline(t, api, adminB, conversationB); items[1].Delivery == nil || items[1].Delivery.Status != "sent" {
		t.Fatalf("workspace B delivery changed: %+v", items[1].Delivery)
	}
}

func TestResendWebhookNeedsAValidSignature(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, resendConfig)
	admin := createSession(t, api, service, "Resend Signed", "admin@resend-signed.example.com")
	_, reply, token := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@resend-signed.example.com"), "rs@customer.example")
	sentReply(t, pool, reply, time.Now())
	body := resendEvent("email.bounced", platformFrom(token), []string{"z@customer.example"}, "", time.Now())

	unsigned := httptest.NewRequest(http.MethodPost, resendWebhookPath, bytes.NewReader(body))
	// Signed with the right key but under Polar's webhook-* header names.
	polarForm := signedWebhookRequest("msg_polar", body)
	polarForm.URL.Path = resendWebhookPath
	for name, req := range map[string]*http.Request{
		"unsigned":   unsigned,
		"wrong key":  svixRequest("msg_wrong", time.Now().Unix(), "another-signing-key-32-bytes-xx", body),
		"stale":      svixRequest("msg_stale", time.Now().Add(-time.Hour).Unix(), resendWebhookKey, body),
		"polar form": polarForm,
	} {
		if response := serve(api, req); response.Code != http.StatusUnauthorized {
			t.Errorf("%s: status=%d, want 401", name, response.Code)
		}
	}
	if status, _ := deliveryOf(t, pool, reply); status != "sent" {
		t.Fatalf("unsigned event changed the reply: %q", status)
	}

	off, _, _, _ := testAPIWithConfig(t, platformConfig)
	if response := serve(off, signedResendRequest("msg_off", body)); response.Code != http.StatusNotFound {
		t.Fatalf("webhook without RESEND_WEBHOOK_SECRET: status=%d, want 404", response.Code)
	}
}
