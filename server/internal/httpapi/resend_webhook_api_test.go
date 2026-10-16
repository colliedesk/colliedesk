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

// legacyFrom is the From address of replies sent before each reply had its own address.
func legacyFrom(token string) string {
	return "Desk via Colliedesk <reply+" + token + "@mail.colliedesk.test>"
}

// replyFrom is the From address the outbox uses for a reply: the conversation token plus the first
// 12 hex digits of the reply's random Message-ID, so the address names that one reply.
func replyFrom(t *testing.T, pool *pgxpool.Pool, reply string) string {
	t.Helper()
	token := queryString(t, pool, `SELECT c.email_reply_token FROM conversations c JOIN messages m ON m.conversation_id=c.id WHERE m.id=$1`, reply)
	messageID := queryString(t, pool, `SELECT email_message_id FROM messages WHERE id=$1`, reply)
	return "Desk via Colliedesk <reply+" + token + "-" + strings.TrimPrefix(messageID, "cd.")[:12] + "@mail.colliedesk.test>"
}

func TestResendBounceMarksTheReplyDeliveryFailed(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, resendConfig)
	admin := createSession(t, api, service, "Resend Bounce", "admin@resend-bounce.example.com")
	conversation, reply, _ := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@resend-bounce.example.com"), "rb@customer.example")
	sentReply(t, pool, reply, time.Now())

	// The relay rewrites the Message-ID, so the event names one Colliedesk never stored.
	body := resendEvent("email.bounced", replyFrom(t, pool, reply), []string{`"Z" <Z@Customer.Example>`}, "<0102-rewritten@eu-west-1.amazonses.com>", time.Now())
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
}

func TestResendComplaintFailsOnlyTheReplyItNames(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, resendConfig)
	admin := createSession(t, api, service, "Resend Complaint", "admin@resend-complaint.example.com")
	conversation, first, _ := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@resend-complaint.example.com"), "rc@customer.example")
	second := create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Second answer."})
	now := time.Now()
	sentReply(t, pool, first, now.Add(-30*time.Second))
	sentReply(t, pool, second, now)

	body := resendEvent("email.complained", replyFrom(t, pool, first), []string{"z@customer.example"}, "", now.Add(-30*time.Second))
	if status := deliverResend(t, api, "msg_complaint_1", body); status != "applied" {
		t.Fatalf("status=%q, want applied", status)
	}
	if status, lastError := deliveryOf(t, pool, first); status != "failed" || !strings.Contains(lastError, "spam") {
		t.Fatalf("complained reply: status=%q reason=%q", status, lastError)
	}
	if status, _ := deliveryOf(t, pool, second); status != "sent" {
		t.Fatalf("the other reply status=%q, want sent", status)
	}
}

func TestResendRedeliveryFailsNoOtherReply(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, resendConfig)
	admin := createSession(t, api, service, "Resend Redelivery", "admin@resend-redelivery.example.com")
	conversation, first, _ := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@resend-redelivery.example.com"), "rr@customer.example")
	second := create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Second answer."})
	sentReply(t, pool, first, time.Now().Add(-3*time.Hour))
	sentReply(t, pool, second, time.Now())

	body := resendEvent("email.bounced", replyFrom(t, pool, second), []string{"z@customer.example"}, "", time.Now())
	if status := deliverResend(t, api, "msg_redelivered", body); status != "applied" {
		t.Fatalf("first delivery status=%q, want applied", status)
	}
	// The agent retries the bounced reply; a late redelivery of the same event must not fail it again.
	if _, err := pool.Exec(t.Context(), `UPDATE email_outbox SET status='sent',last_error=NULL WHERE message_id=$1`, second); err != nil {
		t.Fatal(err)
	}
	if status := deliverResend(t, api, "msg_redelivered", body); status != "duplicate" {
		t.Fatalf("redelivery status=%q, want duplicate", status)
	}
	for name, reply := range map[string]string{"older delivered reply": first, "retried reply": second} {
		if status, _ := deliveryOf(t, pool, reply); status != "sent" {
			t.Errorf("%s status=%q, want sent", name, status)
		}
	}
}

func TestResendBounceBeforeTheReplyIsMarkedSentStillFailsIt(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, resendConfig)
	admin := createSession(t, api, service, "Resend Race", "admin@resend-race.example.com")
	_, reply, _ := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@resend-race.example.com"), "rrace@customer.example")
	// The relay accepted the reply and reported a suppressed address before the worker recorded the send.
	if _, err := pool.Exec(t.Context(), `UPDATE email_outbox SET attempts=1,locked_until=now()+interval '5 minutes',claim_token=gen_random_uuid() WHERE message_id=$1`, reply); err != nil {
		t.Fatal(err)
	}
	body := resendEvent("email.bounced", replyFrom(t, pool, reply), []string{"z@customer.example"}, "", time.Now())
	if status := deliverResend(t, api, "msg_race", body); status != "applied" {
		t.Fatalf("status=%q, want applied", status)
	}
	if status, _ := deliveryOf(t, pool, reply); status != "failed" {
		t.Fatalf("in-flight reply status=%q, want failed", status)
	}
}

func TestResendLegacyReplyAddressMatchesOnlyByMessageID(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, resendConfig)
	admin := createSession(t, api, service, "Resend Legacy", "admin@resend-legacy.example.com")
	conversation, first, token := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@resend-legacy.example.com"), "rl@customer.example")
	second := create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Second answer."})
	sentReply(t, pool, first, time.Now().Add(-time.Minute))
	sentReply(t, pool, second, time.Now())

	// The old address names only the conversation: without our Message-ID nothing says which reply.
	guess := resendEvent("email.bounced", legacyFrom(token), []string{"z@customer.example"}, "<rewritten@eu-west-1.amazonses.com>", time.Now())
	if status := deliverResend(t, api, "msg_legacy_guess", guess); status != "ignored" {
		t.Fatalf("legacy event without our Message-ID: status=%q, want ignored", status)
	}
	ourID := queryString(t, pool, `SELECT email_message_id FROM messages WHERE id=$1`, first)
	exact := resendEvent("email.bounced", legacyFrom(token), []string{"z@customer.example"}, "<"+ourID+">", time.Now())
	if status := deliverResend(t, api, "msg_legacy_exact", exact); status != "applied" {
		t.Fatalf("legacy event with our Message-ID: status=%q, want applied", status)
	}
	if status, _ := deliveryOf(t, pool, first); status != "failed" {
		t.Fatalf("named reply status=%q, want failed", status)
	}
	if status, _ := deliveryOf(t, pool, second); status != "sent" {
		t.Fatalf("other reply status=%q, want sent", status)
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
	fromA := replyFrom(t, pool, replyA)
	ourIDB := queryString(t, pool, `SELECT email_message_id FROM messages WHERE id=$1`, replyB)

	for name, body := range map[string][]byte{
		// A reply goes to the conversation's contact; a bounce that names anyone else, alone or next
		// to the contact, cannot say the contact's copy failed.
		"recipient we never sent to":     resendEvent("email.bounced", fromA, []string{"cc@customer.example"}, "", time.Now()),
		"contact next to a cc recipient": resendEvent("email.bounced", fromA, []string{"z@customer.example", "cc@customer.example"}, "", time.Now()),
		"no recipient":                   resendEvent("email.bounced", fromA, nil, "", time.Now()),
		"unknown token":                  resendEvent("email.bounced", legacyFrom("0123456789abcdef0123456789abcdef"), []string{"z@customer.example"}, "", time.Now()),
		"unknown reply of known token":   resendEvent("email.bounced", "Desk <reply+"+tokenA+"-000000000000@mail.colliedesk.test>", []string{"z@customer.example"}, "", time.Now()),
		"other workspace message id":     resendEvent("email.bounced", legacyFrom(tokenA), []string{"z@customer.example"}, "<"+ourIDB+">", time.Now()),
		"verified support address":       resendEvent("email.bounced", "Desk <support@resend-alpha.example.com>", []string{"z@customer.example"}, "", time.Now()),
		"delivered event":                resendEvent("email.delivered", fromA, []string{"z@customer.example"}, "", time.Now()),
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
	if items := timeline(t, api, adminB, conversationB); items[1].Delivery == nil || items[1].Delivery.Status != "sent" {
		t.Fatalf("workspace B delivery changed: %+v", items[1].Delivery)
	}
}

func TestResendWebhookNeedsAValidSignature(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, resendConfig)
	admin := createSession(t, api, service, "Resend Signed", "admin@resend-signed.example.com")
	_, reply, _ := replyToken(t, api, pool, admin, connectEmail(t, api, admin, "support@resend-signed.example.com"), "rs@customer.example")
	sentReply(t, pool, reply, time.Now())
	body := resendEvent("email.bounced", replyFrom(t, pool, reply), []string{"z@customer.example"}, "", time.Now())

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
