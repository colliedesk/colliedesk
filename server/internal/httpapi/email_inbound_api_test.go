package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const passingAuth = "Authentication-Results: mx.example.net; spf=pass smtp.mailfrom=customer.example; dkim=pass header.d=customer.example; dmarc=pass header.from=customer.example"

func TestInboundNewThreadCreatesConversationAndContact(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Mail Desk", "admin@mail.example.com")
	secret := connectEmail(t, api, admin, "Support@Mail.example.com")

	result := inboundOK(t, api, secret, mail([]string{
		"From: Casey Customer <Casey@Customer.example>",
		"To: support@mail.example.com",
		"Subject: Printer is on fire",
		"Message-ID: <first@customer.example>",
		"Date: Wed, 14 Oct 2026 08:00:00 +0200",
		passingAuth,
	}, "Hello, it is burning.\n"))
	if result.Status != "created" || result.ConversationID == nil || result.MessageID == nil || result.Authentication != "pass" {
		t.Fatalf("unexpected result: %+v", result)
	}

	conversations := conversationList(t, api, admin)
	if len(conversations) != 1 || conversations[0].Subject != "Printer is on fire" || conversations[0].Status != "open" || conversations[0].Channel != "email" {
		t.Fatalf("conversation not created: %+v", conversations)
	}
	contacts := request(t, api, http.MethodGet, "/api/v1/contacts?search=casey", nil, admin, nil)
	if !strings.Contains(contacts.Body.String(), `"email":"casey@customer.example"`) || !strings.Contains(contacts.Body.String(), `"name":"Casey Customer"`) {
		t.Fatalf("contact not created from sender: %s", contacts.Body.String())
	}
	items := timeline(t, api, admin, *result.ConversationID)
	if len(items) != 1 || items[0].Kind != "incoming" || strings.TrimSpace(items[0].Body) != "Hello, it is burning." {
		t.Fatalf("unexpected timeline: %+v", items)
	}
	if items[0].Email == nil || items[0].Email.From != "casey@customer.example" || items[0].Email.Authentication != "pass" || items[0].Delivery != nil {
		t.Fatalf("incoming email metadata missing: %+v", items[0])
	}

	second := inboundOK(t, api, secret, mail([]string{
		"From: casey@customer.example", "To: support@mail.example.com",
		"Subject: Another topic", "Message-ID: <second@customer.example>",
	}, "New question."))
	if second.Status != "created" || *second.ConversationID == *result.ConversationID {
		t.Fatalf("a new thread must create a new conversation: %+v", second)
	}
	var list struct {
		Items []struct{ ID string } `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/contacts?search=customer.example", nil, admin, nil), &list)
	if len(list.Items) != 1 {
		t.Fatalf("contact must be reused by email, got %d contacts", len(list.Items))
	}
}

func TestInboundReplyThreadsByInReplyToAndReopens(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Thread Desk", "admin@thread.example.com")
	secret := connectEmail(t, api, admin, "help@thread.example.com")
	first := inboundOK(t, api, secret, mail([]string{
		"From: dana@customer.example", "To: help@thread.example.com",
		"Subject: Login loop", "Message-ID: <login-1@customer.example>",
	}, "I cannot sign in."))
	conversationID := *first.ConversationID
	_ = create(t, api, admin, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Try the new link."})
	ourMessageID := queryString(t, pool, `SELECT substr(external_id,7) FROM messages WHERE conversation_id=$1 AND kind='outgoing'`, conversationID)
	resolved := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversationID, map[string]any{"status": "resolved"}, admin, nil)
	if resolved.Code != http.StatusOK {
		t.Fatalf("resolve status=%d", resolved.Code)
	}

	reply := inboundOK(t, api, secret, mail([]string{
		"From: dana@customer.example", "To: help@thread.example.com",
		"Subject: Re: Login loop (changed by client)", "Message-ID: <login-2@customer.example>",
		"In-Reply-To: <" + ourMessageID + ">",
		"References: <login-1@customer.example> <" + ourMessageID + ">",
	}, "Still broken."))
	if reply.Status != "threaded" || *reply.ConversationID != conversationID {
		t.Fatalf("reply did not thread: %+v", reply)
	}
	items := timeline(t, api, admin, conversationID)
	if len(items) != 3 || items[2].Kind != "incoming" || items[2].Body != "Still broken." {
		t.Fatalf("unexpected timeline: %+v", items)
	}
	conversations := conversationList(t, api, admin)
	if len(conversations) != 1 || conversations[0].Status != "open" {
		t.Fatalf("customer reply must reopen the resolved conversation: %+v", conversations)
	}

	byReferences := inboundOK(t, api, secret, mail([]string{
		"From: dana@customer.example", "Subject: unrelated subject", "Message-ID: <login-3@customer.example>",
		"References: <unknown@elsewhere.example> <login-1@customer.example>",
	}, "Via references."))
	if byReferences.Status != "threaded" || *byReferences.ConversationID != conversationID {
		t.Fatalf("References header did not thread: %+v", byReferences)
	}
}

func TestInboundReplyThreadsByReplyToken(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Token Desk", "admin@token.example.com")
	secret := connectEmail(t, api, admin, "support@token.example.com")
	first := inboundOK(t, api, secret, mail([]string{
		"From: eli@customer.example", "To: support@token.example.com", "Subject: Refund", "Message-ID: <refund-1@customer.example>",
	}, "Refund please."))
	conversationID := *first.ConversationID
	_ = request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversationID, map[string]any{"status": "closed"}, admin, nil)
	_ = create(t, api, admin, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Done."})
	token := queryString(t, pool, `SELECT substr(key,13) FROM conversation_thread_keys WHERE conversation_id=$1 AND channel_type='email'`, conversationID)

	reply := inboundOK(t, api, secret, mail([]string{
		"From: eli@customer.example", "To: support+r-" + token + "@token.example.com",
		"Subject: thanks", "Message-ID: <refund-2@mobile-client.example>",
	}, "Thanks!"))
	if reply.Status != "threaded" || *reply.ConversationID != conversationID {
		t.Fatalf("reply token did not thread: %+v", reply)
	}
	if conversations := conversationList(t, api, admin); conversations[0].Status != "open" {
		t.Fatalf("closed conversation was not reopened: %+v", conversations)
	}
}

func TestInboundDuplicateMessageIDIsStoredOnce(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Dup Desk", "admin@dup.example.com")
	secret := connectEmail(t, api, admin, "support@dup.example.com")
	raw := mail([]string{"From: fay@customer.example", "Subject: Twice", "Message-ID: <same@customer.example>"}, "Once only.")
	first := inboundOK(t, api, secret, raw)
	second := inboundOK(t, api, secret, raw)
	if first.Status != "created" || second.Status != "duplicate" || *second.MessageID != *first.MessageID {
		t.Fatalf("duplicate not detected: first=%+v second=%+v", first, second)
	}
	if conversations := conversationList(t, api, admin); len(conversations) != 1 {
		t.Fatalf("duplicate created %d conversations", len(conversations))
	}
	if items := timeline(t, api, admin, *first.ConversationID); len(items) != 1 {
		t.Fatalf("duplicate stored %d messages", len(items))
	}
	noID := mail([]string{"From: fay@customer.example", "Subject: No id"}, "Same bytes twice.")
	if inboundOK(t, api, secret, noID).Status != "created" || inboundOK(t, api, secret, noID).Status != "duplicate" {
		t.Fatal("a message without Message-ID must still be idempotent")
	}
}

func TestInboundLoopAndAutoReplyMailIsDropped(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Loop Desk", "admin@loop.example.com")
	secret := connectEmail(t, api, admin, "support@loop.example.com")
	first := inboundOK(t, api, secret, mail([]string{"From: gus@customer.example", "Subject: Real", "Message-ID: <real@customer.example>"}, "Real mail."))
	_ = create(t, api, admin, "/api/v1/conversations/"+*first.ConversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Answer."})
	ourMessageID := queryString(t, pool, `SELECT substr(external_id,7) FROM messages WHERE kind='outgoing'`)

	cases := map[string][]string{
		"auto_submitted": {"From: gus@customer.example", "Subject: Out of office", "Message-ID: <ooo@customer.example>", "Auto-Submitted: auto-replied", "In-Reply-To: <" + ourMessageID + ">"},
		"bulk":           {"From: news@shop.example", "Subject: Sale", "Message-ID: <sale@shop.example>", "Precedence: bulk"},
		"mailer_daemon":  {"From: Mail Delivery System <MAILER-DAEMON@mx.customer.example>", "Subject: Undeliverable", "Message-ID: <dsn@mx.customer.example>"},
		"own_message":    {"From: gus@customer.example", "Subject: Echo", "Message-ID: <" + ourMessageID + ">"},
		"own_address":    {"From: Support <SUPPORT@loop.example.com>", "Subject: Bounced back", "Message-ID: <echo@loop.example.com>"},
	}
	for reason, headers := range cases {
		t.Run(reason, func(t *testing.T) {
			result := inboundOK(t, api, secret, mail(headers, "Automatic text."))
			if result.Status != "dropped" || result.Reason == nil || *result.Reason != reason || result.ConversationID != nil {
				t.Fatalf("want dropped/%s, got %+v", reason, result)
			}
		})
	}
	if conversations := conversationList(t, api, admin); len(conversations) != 1 {
		t.Fatalf("loop mail created conversations: %+v", conversations)
	}
	if items := timeline(t, api, admin, *first.ConversationID); len(items) != 2 {
		t.Fatalf("loop mail was added to the timeline: %+v", items)
	}
}

func TestInboundRejectsMissingWrongAndRetiredSecrets(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Secret Desk", "admin@secret.example.com")
	secret := connectEmail(t, api, admin, "support@secret.example.com")
	raw := mail([]string{"From: hal@customer.example", "Subject: Hi", "Message-ID: <hi@customer.example>"}, "Hi.")
	keyID := secret[:strings.Index(secret, ".")]
	for name, candidate := range map[string]string{
		"missing": "", "garbage": "not-a-secret", "wrong random": keyID + ".AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"session token": admin,
	} {
		if response := inbound(t, api, candidate, raw); response.Code != http.StatusUnauthorized {
			t.Errorf("%s secret status=%d, want 401", name, response.Code)
		}
	}
	rotated := rotateSecret(t, api, admin)
	if response := inbound(t, api, secret, raw); response.Code != http.StatusUnauthorized {
		t.Fatalf("rotated-out secret status=%d, want 401", response.Code)
	}
	if inboundOK(t, api, rotated, raw).Status != "created" {
		t.Fatal("new secret must work")
	}
	if response := request(t, api, http.MethodDelete, "/api/v1/channels/email", nil, admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("disconnect status=%d", response.Code)
	}
	if response := inbound(t, api, rotated, raw); response.Code != http.StatusUnauthorized {
		t.Fatalf("secret of a disconnected channel status=%d, want 401", response.Code)
	}
}

func TestInboundSecretOfWorkspaceACannotWriteIntoWorkspaceB(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	adminA := createSession(t, api, service, "Inbound Alpha", "admin@inbound-alpha.example.com")
	adminB := createSession(t, api, service, "Inbound Beta", "admin@inbound-beta.example.com")
	secretA := connectEmail(t, api, adminA, "support@alpha.example.com")
	secretB := connectEmail(t, api, adminB, "support@beta.example.com")
	threadB := inboundOK(t, api, secretB, mail([]string{
		"From: ivy@customer.example", "To: support@beta.example.com", "Subject: Beta thread", "Message-ID: <beta-1@customer.example>",
	}, "Beta only."))
	_ = create(t, api, adminB, "/api/v1/conversations/"+*threadB.ConversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Beta answer."})
	tokenB := queryString(t, pool, `SELECT substr(key,13) FROM conversation_thread_keys WHERE conversation_id=$1 AND channel_type='email'`, *threadB.ConversationID)
	outgoingB := queryString(t, pool, `SELECT substr(external_id,7) FROM messages WHERE conversation_id=$1 AND kind='outgoing'`, *threadB.ConversationID)

	attack := inboundOK(t, api, secretA, mail([]string{
		"From: ivy@customer.example", "To: support+r-" + tokenB + "@beta.example.com", "Subject: Re: Beta thread",
		"Message-ID: <beta-1@customer.example>", "In-Reply-To: <" + outgoingB + ">", "References: <beta-1@customer.example>",
	}, "Injected through Alpha."))
	if attack.Status != "created" || *attack.ConversationID == *threadB.ConversationID {
		t.Fatalf("Alpha secret must only create in Alpha: %+v", attack)
	}
	if items := timeline(t, api, adminB, *threadB.ConversationID); len(items) != 2 {
		t.Fatalf("Beta timeline changed: %+v", items)
	}
	if response := request(t, api, http.MethodGet, "/api/v1/conversations/"+*attack.ConversationID, nil, adminB, nil); response.Code != http.StatusNotFound {
		t.Fatalf("Alpha conversation visible to Beta: %d", response.Code)
	}
	if conversations := conversationList(t, api, adminA); len(conversations) != 1 {
		t.Fatalf("Alpha should own exactly the injected conversation: %+v", conversations)
	}
}

func TestInboundMarksUnauthenticatedMailAndRecordsAttachments(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Spoof Desk", "admin@spoof.example.com")
	secret := connectEmail(t, api, admin, "support@spoof.example.com")
	failed := inboundOK(t, api, secret, mail([]string{
		"From: ceo@customer.example", "Subject: Wire money", "Message-ID: <spoof@attacker.example>",
		"Authentication-Results: mx.example.net; spf=fail smtp.mailfrom=customer.example; dkim=none; dmarc=fail header.from=customer.example",
	}, "Urgent."))
	if failed.Authentication != "fail" {
		t.Fatalf("dmarc=fail must be marked fail: %+v", failed)
	}
	unknown := inboundOK(t, api, secret, mail([]string{
		"From: jo@customer.example", "Subject: Invoice", "Message-ID: <invoice@customer.example>",
		"MIME-Version: 1.0", `Content-Type: multipart/mixed; boundary="b1"`,
	}, "--b1\nContent-Type: text/plain; charset=utf-8\nContent-Transfer-Encoding: quoted-printable\n\nInvoice attached, caf=C3=A9.\n--b1\n"+
		"Content-Type: application/pdf; name=\"invoice.pdf\"\nContent-Disposition: attachment; filename=\"invoice.pdf\"\nContent-Transfer-Encoding: base64\n\nJVBERi0xLjQK\n--b1--\n"))
	if unknown.Authentication != "none" {
		t.Fatalf("missing Authentication-Results must be none: %+v", unknown)
	}
	items := timeline(t, api, admin, *unknown.ConversationID)
	if len(items) != 1 || strings.TrimSpace(items[0].Body) != "Invoice attached, café." || items[0].Email == nil {
		t.Fatalf("unexpected body: %+v", items)
	}
	attachments := items[0].Email.Attachments
	if len(attachments) != 1 || attachments[0].Filename != "invoice.pdf" || attachments[0].ContentType != "application/pdf" {
		t.Fatalf("attachment metadata not recorded: %+v", attachments)
	}
}

func TestInboundRateLimitsOneContact(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Rate Desk", "admin@rate.example.com")
	secret := connectEmail(t, api, admin, "support@rate.example.com")
	for index := range 10 {
		inboundOK(t, api, secret, mail([]string{"From: kim@customer.example", "Subject: Flood", fmt.Sprintf("Message-ID: <flood-%d@customer.example>", index)}, "Again."))
	}
	response := inbound(t, api, secret, mail([]string{"From: kim@customer.example", "Subject: Flood", "Message-ID: <flood-10@customer.example>"}, "Again."))
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("11th message status=%d headers=%v, want 429 with Retry-After", response.Code, response.Header())
	}
	other := inboundOK(t, api, secret, mail([]string{"From: lee@customer.example", "Subject: Calm", "Message-ID: <calm@customer.example>"}, "Hello."))
	if other.Status != "created" {
		t.Fatalf("rate limit must be per contact: %+v", other)
	}
}

func TestInboundRejectsUnparseableMessage(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Parse Desk", "admin@parse.example.com")
	secret := connectEmail(t, api, admin, "support@parse.example.com")
	if response := inbound(t, api, secret, "this is not an email"); response.Code != http.StatusBadRequest {
		t.Fatalf("garbage status=%d, want 400", response.Code)
	}
	if response := inbound(t, api, secret, mail([]string{"Subject: no sender"}, "x")); response.Code != http.StatusBadRequest {
		t.Fatalf("missing From status=%d, want 400", response.Code)
	}
}
