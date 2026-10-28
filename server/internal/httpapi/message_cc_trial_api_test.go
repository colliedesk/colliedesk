package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// receiveCopied delivers customer mail through the platform forwarder to workspace A, copying cc.
func (f limitsFixture) receiveCopied(t *testing.T, cc string) string {
	t.Helper()
	result := inboundOK(t, f.api, platformSecret, mail([]string{
		"X-Original-To: support@limits-alpha." + hostedDomain,
		"From: customer1@customer.example", "To: support@limits-alpha.example.com", "Cc: " + cc,
		"Subject: Copied question", "Message-ID: <copied-1@customer.example>",
	}, "Adding my colleagues."))
	if result.ConversationID == nil {
		t.Fatalf("fixture: %+v", result)
	}
	return *result.ConversationID
}

func (f limitsFixture) replyCopying(t *testing.T, conversation string, cc ...string) *responseView {
	t.Helper()
	response := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages",
		map[string]any{"kind": "outgoing", "body": "Thanks for writing", "cc": cc}, f.adminA, nil)
	return &responseView{Code: response.Code, Body: response.Body.String()}
}

func TestTrialCopiesOnlyAddressesTheCustomerAlreadyNamed(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.receiveCopied(t, "Boss <boss@customer.example>")
	if got := f.replyCopying(t, conversation, "boss@customer.example", "customer1@customer.example"); got.Code != http.StatusCreated {
		t.Fatalf("copying the customer's own Cc: status=%d body=%s", got.Code, got.Body)
	}
	messages, outbox := f.messageCount(t, f.workspaceA), f.outboxCount(t, f.workspaceA)
	got := f.replyCopying(t, conversation, "boss@customer.example", "stranger@elsewhere.example")
	wantStatus(t, got, http.StatusForbidden, restrictedCode, "copying an address the customer never wrote")
	if !strings.Contains(got.Body, "copy only people the customer wrote from or copied. Not allowed: stranger@elsewhere.example.") {
		t.Fatalf("message must say why: %s", got.Body)
	}
	if f.messageCount(t, f.workspaceA) != messages || f.outboxCount(t, f.workspaceA) != outbox {
		t.Fatal("a refused reply must leave no message and no outbox row")
	}
	multipart := serve(f.api, multipartMessage(t, conversation, f.adminA, "outgoing", "Hi", []upload{{"a.txt", "text/plain", []byte("a")}}, "stranger@elsewhere.example"))
	if multipart.Code != http.StatusForbidden || !strings.Contains(multipart.Body.String(), restrictedCode) {
		t.Fatalf("multipart: status=%d body=%s", multipart.Code, multipart.Body.String())
	}
	if f.messageCount(t, f.workspaceA) != messages {
		t.Fatal("a refused multipart reply must store nothing")
	}
}

func TestTrialDoesNotTrustCopiesFromMailTheWorkspaceWroteItself(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.conversationA(t)
	posted := f.receiveViaWorkspaceSecret(t, "customer1@customer.example", "Cc: victim@elsewhere.example", "In-Reply-To: <limits-1-false@customer.example>")
	if posted.ConversationID == nil || *posted.ConversationID != conversation {
		t.Fatalf("fixture: %+v", posted)
	}
	wantStatus(t, f.replyCopying(t, conversation, "victim@elsewhere.example"), http.StatusForbidden, restrictedCode, "Cc taken from workspace-posted mail")
}

func TestTrialDoesNotTrustCopiesOfMailThatFailedVerification(t *testing.T) {
	f := newLimitsFixture(t, true)
	failed := inboundOK(t, f.api, platformSecret, mail([]string{
		"X-Original-To: support@limits-alpha." + hostedDomain,
		"From: victim@customer.example", "Cc: target@elsewhere.example", "Subject: Forged", "Message-ID: <forged-cc@customer.example>",
		"Authentication-Results: mx.example.net; spf=fail smtp.mailfrom=customer.example; dkim=none; dmarc=fail header.from=customer.example",
	}, "Forged."))
	wantStatus(t, f.replyCopying(t, *failed.ConversationID, "target@elsewhere.example"), http.StatusForbidden, restrictedCode, "reply to dmarc=fail mail")
}

func TestVerifiedDomainOrPaymentLiftsTheCopyRule(t *testing.T) {
	t.Run("verified domain", func(t *testing.T) {
		f := newLimitsFixture(t, true)
		conversation := f.conversationA(t)
		wantStatus(t, f.replyCopying(t, conversation, "new@elsewhere.example"), http.StatusForbidden, restrictedCode, "before verifying")
		f.verifyDomain(t, f.workspaceA)
		if got := f.replyCopying(t, conversation, "new@elsewhere.example"); got.Code != http.StatusCreated {
			t.Fatalf("status=%d body=%s", got.Code, got.Body)
		}
	})
	t.Run("paid", func(t *testing.T) {
		f := newLimitsFixture(t, true)
		conversation := f.conversationA(t)
		f.upgrade(t, f.workspaceA, "sub_cc")
		if got := f.replyCopying(t, conversation, "new@elsewhere.example"); got.Code != http.StatusCreated {
			t.Fatalf("status=%d body=%s", got.Code, got.Body)
		}
	})
}

func TestSelfHostedCopiesAnyone(t *testing.T) {
	f := newLimitsFixture(t, false)
	if got := f.replyCopying(t, f.conversationA(t), "new@elsewhere.example"); got.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", got.Code, got.Body)
	}
}

func TestCopyingStillCountsOnTheTrialDailyCap(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.receiveCopied(t, "boss@customer.example")
	f.seedOutbox(t, f.workspaceA, 200, "sent", f.clock.Now().Add(-time.Hour))
	wantStatus(t, f.replyCopying(t, conversation, "boss@customer.example"), http.StatusTooManyRequests, limitCode, "reply at the cap")
}

// A sender writes the Cc line of their own mail, so a trial user can mail themselves with strangers
// copied and then copy all of them on every reply. The daily cap must count each person reached.
func TestTrialCapCountsEveryRecipientNotEveryReply(t *testing.T) {
	f := newLimitsFixture(t, true)
	victims := make([]string, 10)
	for i := range victims {
		victims[i] = fmt.Sprintf("victim%d@elsewhere.example", i+1)
	}
	conversation := f.receiveCopied(t, strings.Join(victims, ", "))
	// Each reply reaches the contact and ten copies: 11 recipients. 18 replies reach 198.
	for i := range 18 {
		if got := f.replyCopying(t, conversation, victims...); got.Code != http.StatusCreated {
			t.Fatalf("reply %d: status=%d body=%s", i+1, got.Code, got.Body)
		}
	}
	wantStatus(t, f.replyCopying(t, conversation, victims...), http.StatusTooManyRequests, limitCode, "a reply that would reach 209 people")
	f.wantReplyQueued(t, conversation, "a reply to the contact alone reaches the 199th person")
	wantStatus(t, f.replyCopying(t, conversation, victims[0]), http.StatusTooManyRequests, limitCode, "a reply to 2 people with 1 left")
	if usage := f.usage(t, f.adminA); usage.Usage.RepliesLast24h != 199 {
		t.Fatalf("usage.replies_last_24h=%d, want the 199 people reached", usage.Usage.RepliesLast24h)
	}
}

func TestTrialCopyRefusalNamesEveryRefusedAddress(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.receiveCopied(t, "boss@customer.example")
	got := f.replyCopying(t, conversation, "boss@customer.example", "one@elsewhere.example", "two@elsewhere.example")
	wantStatus(t, got, http.StatusForbidden, restrictedCode, "copying two strangers")
	var body struct {
		Error struct {
			Message   string   `json:"message"`
			RefusedCC []string `json:"refused_cc"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(got.Body), &body); err != nil {
		t.Fatal(err)
	}
	if want := []string{"one@elsewhere.example", "two@elsewhere.example"}; !reflect.DeepEqual(body.Error.RefusedCC, want) {
		t.Fatalf("refused_cc=%v, want %v", body.Error.RefusedCC, want)
	}
	if !strings.Contains(body.Error.Message, "one@elsewhere.example, two@elsewhere.example") || strings.Contains(body.Error.Message, "boss@") {
		t.Fatalf("the message must name exactly the refused addresses: %q", body.Error.Message)
	}
	refusedContact := f.reply(t, f.adminA, f.agentStarted(t))
	wantStatus(t, refusedContact, http.StatusForbidden, restrictedCode, "reply in an agent-started conversation")
	if strings.Contains(refusedContact.Body, "refused_cc") {
		t.Fatalf("a refusal about the contact names no Cc address: %s", refusedContact.Body)
	}
}

// Anyone can post a delivery status report to the workspace's own inbound endpoint. Failing a reply
// that already left must not hand its recipients back to the daily cap.
func TestABouncedReplyStillCountsOnTheTrialCap(t *testing.T) {
	f := newLimitsFixture(t, true)
	victims := make([]string, 10)
	for i := range victims {
		victims[i] = fmt.Sprintf("victim%d@elsewhere.example", i+1)
	}
	conversation := f.receiveCopied(t, strings.Join(victims, ", "))
	var last string
	for i := range 18 {
		got := f.replyCopying(t, conversation, victims...)
		if got.Code != http.StatusCreated {
			t.Fatalf("reply %d: status=%d body=%s", i+1, got.Code, got.Body)
		}
		var created struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal([]byte(got.Body), &created); err != nil {
			t.Fatal(err)
		}
		last = created.ID
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE channel_outbox SET status='sent',sent_at=$2,attempts=1 WHERE workspace_id=$1`,
		f.workspaceA, f.clock.Now()); err != nil {
		t.Fatal(err)
	}
	ourID := queryString(t, f.pool, `SELECT substr(external_id,7) FROM messages WHERE id=$1`, last)
	inboundOK(t, f.api, f.secretA, mail([]string{
		"From: Mail Delivery System <MAILER-DAEMON@mx.customer.example>", "Subject: Undelivered Mail Returned to Sender",
		"Message-ID: <forged-dsn@mx.customer.example>", "MIME-Version: 1.0",
		`Content-Type: multipart/report; report-type=delivery-status; boundary="r1"`,
	}, "--r1\nContent-Type: message/delivery-status\n\nReporting-MTA: dns; mx.customer.example\n\n"+
		"Final-Recipient: rfc822; customer1@customer.example\nAction: failed\nStatus: 5.1.1\n"+
		"--r1\nContent-Type: text/rfc822-headers\n\nMessage-ID: <"+ourID+">\n--r1--\n"))
	if status := queryString(t, f.pool, `SELECT status FROM channel_outbox WHERE message_id=$1`, last); status != "failed" {
		t.Fatalf("fixture: the forged report must fail the reply, status=%s", status)
	}
	if usage := f.usage(t, f.adminA); usage.Usage.RepliesLast24h != 198 {
		t.Fatalf("usage.replies_last_24h=%d, want 198: a reply that left still reached its 11 people", usage.Usage.RepliesLast24h)
	}
	wantStatus(t, f.replyCopying(t, conversation, victims...), http.StatusTooManyRequests, limitCode, "a reply past 200 after a forged bounce")
}

// A retry sends the reply to everyone again, so it counts from the moment it is queued, on top of the
// earlier send it repeats while that one is still in the window.
func TestARetriedReplyCountsAgainFromItsOwnSendTime(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := f.receiveCopied(t, "boss@customer.example, ops@customer.example")
	got := f.replyCopying(t, conversation, "boss@customer.example", "ops@customer.example")
	if got.Code != http.StatusCreated {
		t.Fatalf("reply: status=%d body=%s", got.Code, got.Body)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(got.Body), &created); err != nil {
		t.Fatal(err)
	}
	bounce := func() {
		t.Helper()
		if _, err := f.pool.Exec(context.Background(), `UPDATE channel_outbox SET status='failed',sent_at=$2,last_error='bounced' WHERE message_id=$1`,
			created.ID, f.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	start := f.clock.Now()
	bounce()
	f.clock.set(start.Add(23 * time.Hour))
	if got := f.retry(t, conversation, created.ID); got.Code != http.StatusOK {
		t.Fatalf("retry: status=%d body=%s", got.Code, got.Body)
	}
	if usage := f.usage(t, f.adminA); usage.Usage.RepliesLast24h != 6 {
		t.Fatalf("after the retry usage=%d, want 6: the first send and the resend each reached 3 people", usage.Usage.RepliesLast24h)
	}
	bounce()
	f.clock.set(start.Add(25 * time.Hour))
	if usage := f.usage(t, f.adminA); usage.Usage.RepliesLast24h == 0 {
		t.Fatal("two hours after the resend it must still count")
	}
}
