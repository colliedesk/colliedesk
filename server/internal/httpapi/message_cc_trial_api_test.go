package httpapi_test

import (
	"net/http"
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
	if !strings.Contains(got.Body, "wrote to you first") {
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
