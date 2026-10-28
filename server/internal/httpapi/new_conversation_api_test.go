package httpapi_test

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
)

// multipartConversation builds a new-conversation request; each cc address is its own field, ahead of the files.
func multipartConversation(t *testing.T, token, contactID, subject, body string, files []upload, cc ...string) *http.Request {
	t.Helper()
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, field := range [][2]string{{"contact_id", contactID}, {"subject", subject}, {"body", body}} {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	for _, address := range cc {
		if err := writer.WriteField("cc", address); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range files {
		header := textproto.MIMEHeader{}
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename=%q`, file.name))
		header.Set("Content-Type", file.contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", &buffer)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestNewConversationEmailsTheContactAndTheirReplyThreadsBack(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Outbound Start", "admin@outbound-start.example.com")
	secret := connectEmail(t, api, admin, "support@outbound-start.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Pat", "email": "pat@prospect.example"})

	response := request(t, api, http.MethodPost, "/api/v1/conversations", map[string]any{
		"contact_id": contact, "subject": "Your renewal", "body": "Your plan renews next week.", "cc": []string{"Boss@Prospect.example"},
	}, admin, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var conversation struct {
		ID      string `json:"id"`
		Subject string `json:"subject"`
	}
	decode(t, response, &conversation)
	items := timeline(t, api, admin, conversation.ID)
	if len(items) != 1 || items[0].Kind != "outgoing" || items[0].Body != "Your plan renews next week." || items[0].Delivery == nil || items[0].Delivery.Status != "queued" {
		t.Fatalf("timeline=%+v, want the first reply queued for email", items)
	}
	if cc := queryString(t, pool, `SELECT array_to_string(cc,',') FROM messages WHERE conversation_id=$1`, conversation.ID); cc != "boss@prospect.example" {
		t.Fatalf("stored cc=%q", cc)
	}
	ours := queryString(t, pool, `SELECT substr(external_id,7) FROM messages WHERE conversation_id=$1`, conversation.ID)

	answer := inboundOK(t, api, secret, mail([]string{
		"From: pat@prospect.example", "To: support@outbound-start.example.com", "Subject: Re: Your renewal",
		"Message-ID: <renewal-answer@prospect.example>", "In-Reply-To: <" + ours + ">",
	}, "Please renew."))
	if answer.Status != "threaded" || answer.ConversationID == nil || *answer.ConversationID != conversation.ID {
		t.Fatalf("customer answer=%+v, want it in conversation %s", answer, conversation.ID)
	}
}

func TestNewConversationThatCannotBeSentStoresNothing(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "No Channel", "admin@no-channel.example.com")
	other := createSession(t, api, service, "No Channel Other", "admin@no-channel-other.example.com")
	connectEmail(t, api, other, "support@no-channel-other.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Pat", "email": "pat@prospect.example"})
	foreign := create(t, api, other, "/api/v1/contacts", map[string]any{"name": "Zed", "email": "zed@prospect.example"})
	nothing := func(what string) {
		t.Helper()
		if got := queryString(t, pool, `SELECT count(*)::text FROM conversations`); got != "0" {
			t.Fatalf("%s: %s conversations stored, want none", what, got)
		}
		if got := queryString(t, pool, `SELECT count(*)::text FROM messages`); got != "0" {
			t.Fatalf("%s: %s messages stored, want none", what, got)
		}
	}
	send := func(token, contactID string, extra map[string]any) *httptest.ResponseRecorder {
		body := map[string]any{"contact_id": contactID, "subject": "Hello", "body": "First words."}
		for key, value := range extra {
			body[key] = value
		}
		return request(t, api, http.MethodPost, "/api/v1/conversations", body, token, nil)
	}
	if got := send(admin, contact, nil); got.Code != http.StatusConflict || !strings.Contains(got.Body.String(), "email_channel_missing") {
		t.Fatalf("without an email channel status=%d body=%s, want 409 email_channel_missing", got.Code, got.Body.String())
	}
	nothing("no email channel")
	if got := send(other, contact, nil); got.Code != http.StatusNotFound {
		t.Fatalf("another workspace's contact answered %d, want 404", got.Code)
	}
	if got := send(other, foreign, map[string]any{"body": "  "}); got.Code != http.StatusBadRequest {
		t.Fatalf("an empty first reply answered %d, want 400", got.Code)
	}
	if got := send(other, foreign, map[string]any{"cc": []string{"support@no-channel-other.example.com"}}); got.Code != http.StatusBadRequest {
		t.Fatalf("copying our own address answered %d, want 400", got.Code)
	}
	nothing("refused calls")
	if got := request(t, api, http.MethodPost, "/api/v1/conversations", map[string]any{"contact_id": foreign, "subject": "Hello", "cc": []string{"a@b.example"}}, other, nil); got.Code != http.StatusBadRequest {
		t.Fatalf("cc without a body answered %d, want 400", got.Code)
	}
}

// A token below send gets an agent request; the person approving it must see who will be emailed, and a
// token that may not send cannot slip extra recipients past them.
func TestNewConversationFromATokenBelowSendNamesEveryRecipientForApproval(t *testing.T) {
	api, service, _ := testAPIWithPool(t)
	admin := createSession(t, api, service, "Approve Start", "admin@approve-start.example.com")
	other := createSession(t, api, service, "Approve Start Other", "admin@approve-start-other.example.com")
	connectEmail(t, api, admin, "support@approve-start.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Pat Prospect", "email": "pat@prospect.example"})
	foreign := create(t, api, other, "/api/v1/contacts", map[string]any{"name": "Zed", "email": "zed@elsewhere.example"})
	draft := issueToken(t, api, admin, "draft starter", "draft").Token

	response := request(t, api, http.MethodPost, "/api/v1/conversations", map[string]any{
		"contact_id": contact, "subject": "Your renewal", "body": "It renews next week.",
	}, draft, nil)
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s, want 202", response.Code, response.Body.String())
	}
	var filed struct {
		Summary string `json:"summary"`
	}
	decode(t, response, &filed)
	for _, want := range []string{"Start a conversation by email", "pat@prospect.example", "Your renewal", "It renews next week."} {
		if !strings.Contains(filed.Summary, want) {
			t.Errorf("summary %q lacks %q", filed.Summary, want)
		}
	}
	if got := request(t, api, http.MethodPost, "/api/v1/conversations", map[string]any{
		"contact_id": contact, "subject": "Hello", "body": "Hi", "cc": []string{"someone@else.example"},
	}, draft, nil); got.Code != http.StatusBadRequest {
		t.Errorf("a draft token copying someone answered %d, want 400", got.Code)
	}
	added := request(t, api, http.MethodPost, "/api/v1/conversations", map[string]any{
		"contact": map[string]any{"name": "Ned New", "email": "Ned@New.example"}, "subject": "Welcome", "body": "Hello Ned.",
	}, draft, nil)
	decode(t, added, &filed)
	if added.Code != http.StatusAccepted || !strings.Contains(filed.Summary, "<ned@new.example>, a new customer") {
		t.Errorf("a new customer's request %d summary %q must name the address and say it is new", added.Code, filed.Summary)
	}
	if got := request(t, api, http.MethodPost, "/api/v1/conversations", map[string]any{
		"contact_id": foreign, "subject": "Hello", "body": "Hi",
	}, draft, nil); got.Code != http.StatusNotFound {
		t.Errorf("a request for another workspace's contact answered %d, want 404", got.Code)
	}
}

// A customer added while starting a conversation is written in the same transaction: a refused email
// leaves neither the conversation nor the new contact, and an address that is already a customer says
// which contact to use.
func TestNewConversationWithANewCustomerIsAllOrNothing(t *testing.T) {
	f := newLimitsFixture(t, true)
	start := func(body map[string]any) *responseView {
		response := request(t, f.api, http.MethodPost, "/api/v1/conversations", body, f.adminA, nil)
		return &responseView{Code: response.Code, Body: response.Body.String()}
	}
	newCustomer := map[string]any{"contact": map[string]any{"name": "Pat Prospect", "email": "Pat@Prospect.example"}, "subject": "Hello", "body": "Want a demo?"}
	wantStatus(t, start(newCustomer), http.StatusForbidden, restrictedCode, "a trial email to a new customer")
	if count := queryString(t, f.pool, `SELECT count(*)::text FROM contacts WHERE workspace_id=$1 AND email='pat@prospect.example'`, f.workspaceA); count != "0" {
		t.Fatalf("a refused conversation left the new contact behind (%s rows)", count)
	}
	f.verifyDomain(t, f.workspaceA)
	created := start(newCustomer)
	if created.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s, want 201", created.Code, created.Body)
	}
	if name := queryString(t, f.pool, `SELECT k.name FROM conversations c JOIN contacts k ON k.id=c.contact_id WHERE c.workspace_id=$1 AND k.email='pat@prospect.example'`, f.workspaceA); name != "Pat Prospect" {
		t.Fatalf("contact name=%q", name)
	}
	again := start(newCustomer)
	if again.Code != http.StatusConflict || !strings.Contains(again.Body, `"code":"contact_exists"`) || !strings.Contains(again.Body, `"email":"pat@prospect.example"`) {
		t.Fatalf("an existing address answered %d %s, want 409 contact_exists with the contact", again.Code, again.Body)
	}
	for _, bad := range []map[string]any{
		{"contact": map[string]any{"name": "", "email": "x@y.example"}, "subject": "Hello", "body": "Hi"},
		{"contact": map[string]any{"name": "X", "email": "not-an-address"}, "subject": "Hello", "body": "Hi"},
		{"contact_id": "00000000-0000-4000-8000-000000000000", "contact": map[string]any{"name": "X", "email": "x@y.example"}, "subject": "Hello", "body": "Hi"},
	} {
		if got := start(bad); got.Code != http.StatusBadRequest {
			t.Errorf("%v answered %d %s, want 400", bad, got.Code, got.Body)
		}
	}
}

func TestNewConversationWithFilesCanAddTheCustomer(t *testing.T) {
	f := newAttachFixture(t)
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	for _, field := range [][2]string{{"contact_name", "Quinn New"}, {"contact_email", "quinn.new@prospect.example"}, {"subject", "Your quote"}, {"body", "Attached."}} {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	part, err := writer.CreateFormFile("files", "quote.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("quote")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations", &buffer)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+f.tokenA)
	if response := serve(f.api, req); response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if got := queryString(t, f.pool, `SELECT k.name||':'||a.filename FROM contacts k JOIN conversations c ON c.contact_id=k.id
		JOIN messages m ON m.conversation_id=c.id JOIN attachments a ON a.message_id=m.id WHERE k.email='quinn.new@prospect.example'`); got != "Quinn New:quote.txt" {
		t.Fatalf("stored=%q", got)
	}
	if channel := queryString(t, f.pool, `SELECT c.channel FROM conversations c JOIN contacts k ON k.id=c.contact_id WHERE k.email='quinn.new@prospect.example'`); channel != "agent" {
		t.Fatalf("channel=%q, want agent", channel)
	}
}

func TestANewConversationTheTeamStartsIsOnTheAgentChannel(t *testing.T) {
	api, service, _ := testAPIWithPool(t)
	admin := createSession(t, api, service, "Channel Start", "admin@channel-start.example.com")
	connectEmail(t, api, admin, "support@channel-start.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Pat", "email": "pat@channel-start.example"})
	response := request(t, api, http.MethodPost, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Hello", "body": "First words."}, admin, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var started struct {
		ID      string `json:"id"`
		Channel string `json:"channel"`
	}
	decode(t, response, &started)
	if started.Channel != "agent" {
		t.Fatalf("channel=%q, want agent", started.Channel)
	}
	for filter, want := range map[string]int{"agent": 1, "email": 0} {
		listed := request(t, api, http.MethodGet, "/api/v1/conversations?channel="+filter, nil, admin, nil)
		if listed.Code != http.StatusOK {
			t.Fatalf("channel=%s status=%d body=%s", filter, listed.Code, listed.Body.String())
		}
		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		decode(t, listed, &page)
		if len(page.Items) != want || (want == 1 && page.Items[0].ID != started.ID) {
			t.Fatalf("channel=%s listed %+v, want %d", filter, page.Items, want)
		}
	}
}

func TestTrialRefusesNewOutboundConversationsUntilTheDomainIsVerified(t *testing.T) {
	f := newLimitsFixture(t, true)
	contact := create(t, f.api, f.adminA, "/api/v1/contacts", map[string]any{"name": "Cold Lead", "email": "lead@cold.example"})
	start := func() *responseView {
		response := request(t, f.api, http.MethodPost, "/api/v1/conversations",
			map[string]any{"contact_id": contact, "subject": "Hello from us", "body": "Want a demo?"}, f.adminA, nil)
		return &responseView{Code: response.Code, Body: response.Body.String()}
	}
	got := start()
	wantStatus(t, got, http.StatusForbidden, restrictedCode, "a new conversation on the trial")
	if !strings.Contains(got.Body, "wrote to you first") {
		t.Fatalf("the refusal must say why and how out: %s", got.Body)
	}
	if count := queryString(t, f.pool, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`, f.workspaceA); count != "0" {
		t.Fatalf("a refused new conversation left %s conversations", count)
	}
	if count := f.outboxCount(t, f.workspaceA); count != "0" {
		t.Fatalf("outbox=%s, want nothing queued", count)
	}
	f.verifyDomain(t, f.workspaceA)
	if got := start(); got.Code != http.StatusCreated {
		t.Fatalf("with a verified domain status=%d body=%s, want 201", got.Code, got.Body)
	}
}

func TestNewConversationCarriesItsFirstReplyFiles(t *testing.T) {
	f := newAttachFixture(t)
	contact := create(t, f.api, f.tokenA, "/api/v1/contacts", map[string]any{"name": "Quinn", "email": "quinn@prospect.example"})
	pdf := []byte("%PDF-1.7 quote")
	response := serve(f.api, multipartConversation(t, f.tokenA, contact, "Your quote", "The quote is attached.",
		[]upload{{"quote.pdf", "application/pdf", pdf}}, "finance@prospect.example"))
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var conversation struct {
		ID string `json:"id"`
	}
	decode(t, response, &conversation)
	if got := queryString(t, f.pool, `SELECT a.filename||':'||a.sha256||':'||array_to_string(m.cc,',') FROM attachments a JOIN messages m ON m.id=a.message_id
		WHERE m.conversation_id=$1`, conversation.ID); got != "quote.pdf:"+sum(pdf)+":finance@prospect.example" {
		t.Fatalf("stored first reply=%q", got)
	}

	// A refused multipart conversation leaves neither rows nor bytes.
	before := storedObjects(t, f.root)
	other := create(t, f.api, f.tokenB, "/api/v1/contacts", map[string]any{"name": "Rex", "email": "rex@prospect.example"})
	refused := serve(f.api, multipartConversation(t, f.tokenB, other, "No channel", "Hello.", []upload{{"a.txt", "text/plain", []byte("a")}}))
	if refused.Code != http.StatusConflict {
		t.Fatalf("multipart without an email channel status=%d body=%s, want 409", refused.Code, refused.Body.String())
	}
	if got := storedObjects(t, f.root); got != before {
		t.Fatalf("stored objects %d → %d after a refused conversation", before, got)
	}
	if got := queryString(t, f.pool, `SELECT count(*)::text FROM conversations WHERE subject='No channel'`); got != "0" {
		t.Fatalf("refused multipart conversation stored %s rows", got)
	}
}
