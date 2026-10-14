package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

func TestInboundTrustsOnlyTheConfiguredAuthServID(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Forge Desk", "admin@forge.example.com")
	secret := connectEmail(t, api, admin, "support@forge.example.com")
	forged := inboundOK(t, api, secret, mail([]string{
		"From: ceo@customer.example", "Subject: Wire money", "Message-ID: <forged@attacker.example>",
		"Authentication-Results: attacker.example; spf=pass; dkim=pass; dmarc=pass",
	}, "Urgent."))
	if forged.Authentication != "none" {
		t.Fatalf("sender-supplied verdict was trusted: %+v", forged)
	}
	stamped := inboundOK(t, api, secret, mail([]string{
		"From: ceo@customer.example", "Subject: Real", "Message-ID: <real@customer.example>",
		"Authentication-Results: mx.example.net; dmarc=pass", "Authentication-Results: attacker.example; dmarc=fail",
	}, "Real."))
	if stamped.Authentication != "pass" {
		t.Fatalf("trusted stamp ignored: %+v", stamped)
	}

	untrusting, untrustingService, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Email.TrustedAuthServID = "" })
	other := createSession(t, untrusting, untrustingService, "No Trust Desk", "admin@notrust.example.com")
	otherSecret := connectEmail(t, untrusting, other, "support@notrust.example.com")
	if result := inboundOK(t, untrusting, otherSecret, mail([]string{"From: a@customer.example", "Subject: x", "Message-ID: <x@customer.example>", passingAuth}, "x")); result.Authentication != "none" {
		t.Fatalf("without a trusted authserv-id every verdict must be none: %+v", result)
	}
}

func TestInboundAcceptsCustomerMailThroughAGoogleGroup(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Group Desk", "admin@group.example.com")
	secret := connectEmail(t, api, admin, "support@group.example.com")
	result := inboundOK(t, api, secret, mail([]string{
		"From: Pat Customer <pat@customer.example>", "To: support@group.example.com", "Subject: Login help",
		"Message-ID: <group-1@customer.example>", "List-Id: <support.group.example.com>", "Precedence: list",
		"Mailing-list: list support@group.example.com; contact support+owners@group.example.com",
		"List-Post: <mailto:support@group.example.com>",
		"List-Unsubscribe: <mailto:googlegroups-manage+123+unsubscribe@googlegroups.com>",
	}, "I cannot log in."))
	if result.Status != "created" {
		t.Fatalf("mail through a Google Group was not ingested: %+v", result)
	}
	newsletter := inboundOK(t, api, secret, mail([]string{
		"From: news@shop.example", "Subject: Sale", "Message-ID: <sale-2@shop.example>", "List-Id: <news.shop.example>",
		"List-Unsubscribe: <https://shop.example/u>", "List-Unsubscribe-Post: List-Unsubscribe=One-Click",
	}, "50% off."))
	if newsletter.Status != "dropped" || *newsletter.Reason != "bulk" {
		t.Fatalf("bulk newsletter must still be dropped: %+v", newsletter)
	}
}

func TestInboundBoundsHostileThreadHeaders(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Hostile Desk", "admin@hostile.example.com")
	secret := connectEmail(t, api, admin, "support@hostile.example.com")
	long := "<" + strings.Repeat("a", 4000) + "@customer.example>"
	raw := mail([]string{"From: q@customer.example", "Subject: Long id", "Message-ID: " + long}, "Long.")
	if first := inboundOK(t, api, secret, raw); first.Status != "created" {
		t.Fatalf("long Message-ID: %+v", first)
	}
	if again := inboundOK(t, api, secret, raw); again.Status != "duplicate" {
		t.Fatalf("long Message-ID must stay idempotent: %+v", again)
	}
	var references strings.Builder
	for index := range 5000 {
		fmt.Fprintf(&references, "<ref-%d@elsewhere.example> ", index)
	}
	started := time.Now()
	result := inboundOK(t, api, secret, mail([]string{"From: q@customer.example", "Subject: Many ids", "Message-ID: <many@customer.example>",
		"In-Reply-To: " + references.String(), "References: " + references.String() + long}, "Many."))
	if result.Status != "threaded" {
		t.Fatalf("a reference to the long id must thread: %+v", result)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("hostile thread headers took %s", elapsed)
	}
}

func TestInboundConcurrentDeliveriesOfOneMessageStoreItOnce(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Race Desk", "admin@race.example.com")
	secret := connectEmail(t, api, admin, "support@race.example.com")
	for round := range 5 {
		id := fmt.Sprintf("Message-ID: <race-%d@customer.example>", round)
		var wait sync.WaitGroup
		statuses := make([]string, 8)
		for index := range statuses {
			wait.Add(1)
			go func() {
				defer wait.Done()
				// Different senders take different locks, so only the unique index can stop the second insert.
				from := fmt.Sprintf("From: racer-%d@customer.example", index%2)
				response := serve(api, inboundRequest(t, mail([]string{from, "Subject: Race", id}, "Once."), secret))
				var result inboundResult
				if response.Code == http.StatusOK {
					decode(t, response, &result)
				}
				statuses[index] = fmt.Sprintf("%d/%s", response.Code, result.Status)
			}()
		}
		wait.Wait()
		created := 0
		for _, status := range statuses {
			switch status {
			case "200/created":
				created++
			case "200/duplicate":
			default:
				t.Fatalf("round %d: unexpected outcome %q in %v", round, status, statuses)
			}
		}
		if created != 1 {
			t.Fatalf("round %d: %d deliveries created a message: %v", round, created, statuses)
		}
	}
	if conversations := conversationList(t, api, admin); len(conversations) != 5 {
		t.Fatalf("want 5 conversations, got %d", len(conversations))
	}
}

func TestInboundCapsNewConversationsPerWorkspace(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Ceiling Desk", "admin@ceiling.example.com")
	secret := connectEmail(t, api, admin, "support@ceiling.example.com")
	existing := inboundOK(t, api, secret, mail([]string{"From: r@customer.example", "Subject: Existing", "Message-ID: <existing@customer.example>"}, "Hi."))
	if _, err := pool.Exec(t.Context(), `INSERT INTO conversations(workspace_id,contact_id,subject)
		SELECT workspace_id,contact_id,'Flood '||n FROM conversations, generate_series(1,200) n WHERE id=$1`, *existing.ConversationID); err != nil {
		t.Fatal(err)
	}
	response := inbound(t, api, secret, mail([]string{"From: fresh@customer.example", "Subject: New", "Message-ID: <fresh@customer.example>"}, "New."))
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("new thread over the workspace ceiling status=%d, want 429", response.Code)
	}
	reply := inboundOK(t, api, secret, mail([]string{"From: r@customer.example", "Subject: Re: Existing", "Message-ID: <existing-2@customer.example>",
		"In-Reply-To: <existing@customer.example>"}, "Still here."))
	if reply.Status != "threaded" {
		t.Fatalf("replies on existing threads must pass the ceiling: %+v", reply)
	}
}

func TestInboundBounceMarksTheReplyFailed(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Bounce Desk", "admin@bounce.example.com")
	secret := connectEmail(t, api, admin, "support@bounce.example.com")
	thread := inboundOK(t, api, secret, mail([]string{"From: s@customer.example", "Subject: Bounce", "Message-ID: <bounce-1@customer.example>"}, "Hi."))
	reply := create(t, api, admin, "/api/v1/conversations/"+*thread.ConversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Answer."})
	ourID := queryString(t, pool, `SELECT email_message_id FROM messages WHERE id=$1`, reply)
	if _, err := pool.Exec(t.Context(), `UPDATE email_outbox SET status='sent',sent_at=now(),attempts=1`); err != nil {
		t.Fatal(err)
	}
	dsn := inboundOK(t, api, secret, mail([]string{
		"From: Mail Delivery System <MAILER-DAEMON@mx.customer.example>", "Subject: Undelivered Mail Returned to Sender",
		"Message-ID: <dsn-1@mx.customer.example>", "MIME-Version: 1.0",
		`Content-Type: multipart/report; report-type=delivery-status; boundary="r1"`,
	}, "--r1\nContent-Type: text/plain\n\nThe mailbox does not exist.\n--r1\nContent-Type: message/delivery-status\n\nStatus: 5.1.1\n"+
		"--r1\nContent-Type: text/rfc822-headers\n\nFrom: support@bounce.example.com\nMessage-ID: <"+ourID+">\nSubject: Re: Bounce\n--r1--\n"))
	if dsn.Status != "dropped" || *dsn.Reason != "mailer_daemon" {
		t.Fatalf("delivery report: %+v", dsn)
	}
	items := timeline(t, api, admin, *thread.ConversationID)
	if items[1].Delivery == nil || items[1].Delivery.Status != "failed" {
		t.Fatalf("bounced reply still shows %+v", items[1].Delivery)
	}
	if lastError := queryString(t, pool, `SELECT last_error FROM email_outbox`); !strings.Contains(lastError, "bounced") {
		t.Fatalf("bounce reason not recorded: %q", lastError)
	}
}

func TestEmailStorageFailuresAreServerErrors(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Outage Desk", "admin@outage.example.com")
	if _, err := pool.Exec(t.Context(), `ALTER TABLE email_channels RENAME TO email_channels_unavailable`); err != nil {
		t.Fatal(err)
	}
	for _, call := range []struct{ method, path string }{
		{http.MethodPut, "/api/v1/channels/email"}, {http.MethodPost, "/api/v1/channels/email/secret"},
	} {
		response := request(t, api, call.method, call.path, map[string]any{"support_address": "support@outage.example.com"}, admin, nil)
		if call.method == http.MethodPost {
			response = request(t, api, call.method, call.path, nil, admin, nil)
		}
		if response.Code != http.StatusInternalServerError {
			t.Errorf("%s %s during a storage failure status=%d, want 500", call.method, call.path, response.Code)
		}
	}
}
