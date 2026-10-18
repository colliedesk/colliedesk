package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pauseSettings struct {
	SendingPause *struct {
		PausedAt string `json:"paused_at"`
		Reason   string `json:"reason"`
	} `json:"sending_pause"`
}

func pauseState(t *testing.T, api http.Handler, token string) pauseSettings {
	t.Helper()
	var settings pauseSettings
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels/email", nil, token, nil), &settings)
	return settings
}

func pauseConfig(mailer *recordingMailer) func(*httpapi.Config) {
	return func(config *httpapi.Config) {
		resendConfig(config)
		config.AlertEmail = "ops@alerts.example.net"
		config.AlertMailer = mailer
	}
}

// sentReplies creates n delivered replies in the workspace, each in its own conversation, and returns
// the reply ids.
func sentReplies(t *testing.T, api http.Handler, pool *pgxpool.Pool, admin, secret, tag string, n int) []string {
	t.Helper()
	replies := make([]string, n)
	for i := range replies {
		_, reply, _ := replyToken(t, api, pool, admin, secret, fmt.Sprintf("%s-%d@customer.example", tag, i))
		sentReply(t, pool, reply, time.Now())
		replies[i] = reply
	}
	return replies
}

var complaintSeq int

// complain delivers a signed Resend complaint for the reply and returns the webhook status.
func complain(t *testing.T, api http.Handler, pool *pgxpool.Pool, reply string) string {
	t.Helper()
	complaintSeq++
	return complainWithID(t, api, pool, reply, fmt.Sprintf("msg_complaint_%d", complaintSeq))
}

func complainWithID(t *testing.T, api http.Handler, pool *pgxpool.Pool, reply, webhookID string) string {
	t.Helper()
	body := resendEvent("email.complained", replyFrom(t, pool, reply), []string{"z@customer.example"}, "<rewritten@eu-west-1.amazonses.com>", time.Now())
	return deliverResend(t, api, webhookID, body)
}

// fillerSent records n more replies as sent in the workspace, as if the team had sent that many.
func fillerSent(t *testing.T, pool *pgxpool.Pool, workspaceID string, n int) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `WITH conv AS (SELECT id FROM conversations WHERE workspace_id=$1 LIMIT 1),
			added AS (INSERT INTO messages(workspace_id,conversation_id,kind,body)
				SELECT $1,conv.id,'outgoing','filler' FROM conv,generate_series(1,$2) RETURNING id)
		INSERT INTO email_outbox(workspace_id,message_id,status,sent_at,attempts) SELECT $1,id,'sent',now(),1 FROM added`, workspaceID, n); err != nil {
		t.Fatal(err)
	}
}

func TestComplaintsBelowTheThresholdDoNotPauseSending(t *testing.T) {
	mailer := &recordingMailer{}
	api, service, pool, _ := testAPIWithConfig(t, pauseConfig(mailer))
	admin := createSession(t, api, service, "Pause Quiet", "admin@pause-quiet.example.com")
	secret := connectEmail(t, api, admin, "support@pause-quiet.example.com")
	replies := sentReplies(t, api, pool, admin, secret, "quiet", 4)

	for _, reply := range replies[:2] {
		if status := complain(t, api, pool, reply); status != "applied" {
			t.Fatalf("complaint status=%q, want applied", status)
		}
	}
	if pauseState(t, api, admin).SendingPause != nil || len(mailer.all()) != 0 {
		t.Fatal("two complaints are under the minimum count and must not pause sending")
	}
}

func TestComplaintsCrossingTheThresholdPauseSendingAndAlertTheOperatorOnce(t *testing.T) {
	mailer := &recordingMailer{}
	api, service, pool, _ := testAPIWithConfig(t, pauseConfig(mailer))
	admin := createSession(t, api, service, "Pause Loud", "admin@pause-loud.example.com")
	secret := connectEmail(t, api, admin, "support@pause-loud.example.com")
	replies := sentReplies(t, api, pool, admin, secret, "loud", 4)

	for _, reply := range replies[:3] {
		complain(t, api, pool, reply)
	}
	paused := pauseState(t, api, admin).SendingPause
	if paused == nil || !strings.HasSuffix(paused.PausedAt, "Z") || !strings.Contains(paused.Reason, "3 spam complaints") {
		t.Fatalf("sending_pause after the third complaint: %+v", paused)
	}
	sent := mailer.all()
	if len(sent) != 1 || len(sent[0].To) != 1 || sent[0].To[0] != "ops@alerts.example.net" || !strings.Contains(sent[0].Message, "Pause Loud") {
		t.Fatalf("operator alerts: %+v", sent)
	}

	complain(t, api, pool, replies[3])
	if len(mailer.all()) != 1 {
		t.Fatalf("a workspace that is already paused must not alert again, got %d mails", len(mailer.all()))
	}
}

func TestADuplicateWebhookDoesNotCountTwice(t *testing.T) {
	mailer := &recordingMailer{}
	api, service, pool, _ := testAPIWithConfig(t, pauseConfig(mailer))
	admin := createSession(t, api, service, "Pause Dup", "admin@pause-dup.example.com")
	secret := connectEmail(t, api, admin, "support@pause-dup.example.com")
	replies := sentReplies(t, api, pool, admin, secret, "dup", 3)

	complainWithID(t, api, pool, replies[0], "msg_dup_a")
	complainWithID(t, api, pool, replies[1], "msg_dup_b")
	for range 3 {
		if status := complainWithID(t, api, pool, replies[1], "msg_dup_b"); status != "duplicate" {
			t.Fatalf("redelivery status=%q, want duplicate", status)
		}
	}
	if pauseState(t, api, admin).SendingPause != nil {
		t.Fatal("two distinct complaints delivered several times each must not pause sending")
	}
}

func TestTheComplaintRateMustBeExceededNotJustTheCount(t *testing.T) {
	mailer := &recordingMailer{}
	api, service, pool, _ := testAPIWithConfig(t, pauseConfig(mailer))
	admin := createSession(t, api, service, "Pause Rate", "admin@pause-rate.example.com")
	secret := connectEmail(t, api, admin, "support@pause-rate.example.com")
	replies := sentReplies(t, api, pool, admin, secret, "rate", 4)
	// 4 replies plus 996 more is 1000 sent: three complaints are exactly 0.3%, which is not over it.
	fillerSent(t, pool, workspaceID(t, api, admin), 996)

	for _, reply := range replies[:3] {
		complain(t, api, pool, reply)
	}
	if pauseState(t, api, admin).SendingPause != nil {
		t.Fatal("3 complaints on 1000 replies is exactly 0.3% and must not pause sending")
	}
	complain(t, api, pool, replies[3])
	if pauseState(t, api, admin).SendingPause == nil {
		t.Fatal("4 complaints on 1000 replies is above 0.3% and must pause sending")
	}
}

func TestComplaintsInOneWorkspaceNeverPauseAnother(t *testing.T) {
	mailer := &recordingMailer{}
	api, service, pool, _ := testAPIWithConfig(t, pauseConfig(mailer))
	adminA := createSession(t, api, service, "Pause Alpha", "admin@pause-alpha.example.com")
	adminB := createSession(t, api, service, "Pause Beta", "admin@pause-beta.example.com")
	secretA := connectEmail(t, api, adminA, "support@pause-alpha.example.com")
	secretB := connectEmail(t, api, adminB, "support@pause-beta.example.com")
	repliesA := sentReplies(t, api, pool, adminA, secretA, "alpha", 2)
	repliesB := sentReplies(t, api, pool, adminB, secretB, "beta", 3)

	for _, reply := range repliesA {
		complain(t, api, pool, reply)
	}
	for _, reply := range repliesB {
		complain(t, api, pool, reply)
	}
	if pauseState(t, api, adminA).SendingPause != nil {
		t.Fatal("B's complaints paused A")
	}
	if pauseState(t, api, adminB).SendingPause == nil {
		t.Fatal("B was not paused by its own complaints")
	}
}
