package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

const mailFromDomain = "send.colliedesk.test"

type ccMessage struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	CC       []string `json:"cc"`
	Delivery *struct {
		Status string `json:"status"`
	} `json:"delivery"`
	Email *struct {
		From string   `json:"from"`
		CC   []string `json:"cc"`
	} `json:"email"`
}

type ccDesk struct {
	api          http.Handler
	pool         *pgxpool.Pool
	admin        string
	other        string
	conversation string
	contact      string
	secret       string
}

// newCCDesk is a hosted workspace "cc.example.com" holding one customer conversation (customer
// ada, who copied boss and our own addresses), and a second workspace.
func newCCDesk(t *testing.T) ccDesk {
	t.Helper()
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) {
		hostedConfig(config)
		config.Email.MailFromDomain = mailFromDomain
	})
	admin := createSession(t, api, service, "Cc Desk", "admin@cc.example.com")
	other := createSession(t, api, service, "Other Cc Desk", "admin@other-cc.example.com")
	secret := connectEmail(t, api, admin, "support@cc.example.com")
	connectEmail(t, api, other, "support@other-cc.example.com")
	result := inboundOK(t, api, secret, mail([]string{
		"From: Ada <ada@customer.example>", "To: support@cc.example.com",
		"Cc: Boss <Boss@Customer.example>, support@cc.example.com, support@cc." + hostedDomain + ", reply+0123456789abcdef0123456789abcdef@" + mailFromDomain,
		"Subject: Invoice", "Message-ID: <cc-start@customer.example>",
	}, "Please help."))
	if result.ConversationID == nil {
		t.Fatalf("fixture: %+v", result)
	}
	return ccDesk{api: api, pool: pool, admin: admin, other: other, conversation: *result.ConversationID, contact: "ada@customer.example", secret: secret}
}

func (d ccDesk) post(t *testing.T, token, conversation string, body map[string]any) *responseView {
	t.Helper()
	response := request(t, d.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", body, token, nil)
	return &responseView{Code: response.Code, Body: response.Body.String()}
}

func (d ccDesk) reply(t *testing.T, cc any) *responseView {
	t.Helper()
	body := map[string]any{"kind": "outgoing", "body": "Copying colleagues."}
	if cc != nil {
		body["cc"] = cc
	}
	return d.post(t, d.admin, d.conversation, body)
}

func (d ccDesk) messages(t *testing.T) []ccMessage {
	t.Helper()
	var list struct {
		Items []ccMessage `json:"items"`
	}
	decode(t, request(t, d.api, http.MethodGet, "/api/v1/conversations/"+d.conversation+"/messages", nil, d.admin, nil), &list)
	return list.Items
}

func TestReplyWithCCIsStoredNormalizedAndReturned(t *testing.T) {
	d := newCCDesk(t)
	got := d.reply(t, []string{"Boss@Customer.example", "ops+billing@customer.example", "boss@customer.example", d.contact})
	if got.Code != http.StatusCreated || !strings.Contains(got.Body, `"cc":["boss@customer.example","ops+billing@customer.example"]`) {
		t.Fatalf("status=%d body=%s, want 201 with the two distinct lowercase addresses (duplicate and the contact dropped)", got.Code, got.Body)
	}
	items := d.messages(t)
	reply := items[len(items)-1]
	if reply.Kind != "outgoing" || len(reply.CC) != 2 || reply.Delivery == nil || reply.Delivery.Status != "queued" {
		t.Fatalf("stored reply = %+v", reply)
	}
	if first := items[0]; first.Kind != "incoming" || len(first.CC) != 0 {
		t.Fatalf("a message without copies lists none: %+v", first)
	}
}

func TestReplyWithoutCCHasAnEmptyList(t *testing.T) {
	d := newCCDesk(t)
	if got := d.reply(t, nil); got.Code != http.StatusCreated || !strings.Contains(got.Body, `"cc":[]`) {
		t.Fatalf("status=%d body=%s", got.Code, got.Body)
	}
}

func TestOnlyAReplyCanBeCopied(t *testing.T) {
	d := newCCDesk(t)
	before := len(d.messages(t))
	got := d.post(t, d.admin, d.conversation, map[string]any{"kind": "note", "body": "Private", "cc": []string{"boss@customer.example"}})
	if got.Code != http.StatusBadRequest {
		t.Fatalf("note with cc: status=%d body=%s, want 400", got.Code, got.Body)
	}
	if len(d.messages(t)) != before {
		t.Fatal("a refused note must not be stored")
	}
}

func TestCCListIsBoundedAndEveryAddressIsPlain(t *testing.T) {
	d := newCCDesk(t)
	many := func(n int) []string {
		list := make([]string, n)
		for i := range list {
			list[i] = fmt.Sprintf("p%d@customer.example", i)
		}
		return list
	}
	if got := d.reply(t, many(11)); got.Code != http.StatusBadRequest {
		t.Fatalf("11 addresses: status=%d body=%s, want 400", got.Code, got.Body)
	}
	if got := d.reply(t, many(10)); got.Code != http.StatusCreated {
		t.Fatalf("10 addresses: status=%d body=%s, want 201", got.Code, got.Body)
	}
	for _, bad := range []string{
		"", "not-an-address", "Boss <boss@customer.example>", "a@customer.example, b@customer.example",
		"a@customer.example\r\nBcc: evil@attacker.example", "a@customer.example\nBcc: evil@attacker.example", "<a@customer.example>",
		strings.Repeat("x", 250) + "@customer.example",
	} {
		before := len(d.messages(t))
		if got := d.reply(t, []string{"ok@customer.example", bad}); got.Code != http.StatusBadRequest {
			t.Errorf("cc %q: status=%d body=%s, want 400", bad, got.Code, got.Body)
		}
		if len(d.messages(t)) != before {
			t.Errorf("cc %q: a refused reply must not be stored", bad)
		}
	}
	for _, wrong := range []any{"boss@customer.example", 7, []any{1}, map[string]any{}} {
		if got := d.reply(t, wrong); got.Code != http.StatusBadRequest {
			t.Errorf("cc %v: status=%d, want 400", wrong, got.Code)
		}
	}
}

func TestCCRefusesOurOwnAddresses(t *testing.T) {
	d := newCCDesk(t)
	for _, own := range []string{
		"support@cc.example.com", "Support@CC.example.com", "support@cc." + hostedDomain, "anything@other." + hostedDomain,
		"reply+0123456789abcdef0123456789abcdef@" + mailFromDomain, "x@" + mailFromDomain,
	} {
		if got := d.reply(t, []string{own}); got.Code != http.StatusBadRequest {
			t.Errorf("cc %q: status=%d body=%s, want 400", own, got.Code, got.Body)
		}
	}
}

func TestMultipartReplyAcceptsRepeatedCCFields(t *testing.T) {
	d := newCCDesk(t)
	request := multipartMessage(t, d.conversation, d.admin, "outgoing", "With a file", []upload{{"a.txt", "text/plain", []byte("a")}}, "boss@customer.example", "ops@customer.example")
	response := serve(d.api, request)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), `"cc":["boss@customer.example","ops@customer.example"]`) {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	bad := serve(d.api, multipartMessage(t, d.conversation, d.admin, "outgoing", "With a file", []upload{{"a.txt", "text/plain", []byte("a")}}, "not-an-address"))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("invalid multipart cc: status=%d body=%s", bad.Code, bad.Body.String())
	}
	note := serve(d.api, multipartMessage(t, d.conversation, d.admin, "note", "Private", nil, "boss@customer.example"))
	if note.Code != http.StatusBadRequest {
		t.Fatalf("multipart note with cc: status=%d body=%s", note.Code, note.Body.String())
	}
}

func TestCCOnAnotherWorkspacesConversationIsNotFoundAndChangesNothing(t *testing.T) {
	d := newCCDesk(t)
	before := len(d.messages(t))
	got := d.post(t, d.other, d.conversation, map[string]any{"kind": "outgoing", "body": "Hi", "cc": []string{"boss@customer.example"}})
	if got.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", got.Code, got.Body)
	}
	multi := serve(d.api, multipartMessage(t, d.conversation, d.other, "outgoing", "Hi", []upload{{"a.txt", "text/plain", []byte("a")}}, "boss@customer.example"))
	if multi.Code != http.StatusNotFound {
		t.Fatalf("multipart: status=%d body=%s, want 404", multi.Code, multi.Body.String())
	}
	if len(d.messages(t)) != before {
		t.Fatal("another workspace's request changed the conversation")
	}
}

func TestInboundMailKeepsItsCcWithoutOurOwnAddresses(t *testing.T) {
	d := newCCDesk(t)
	first := d.messages(t)[0]
	if first.Email == nil || fmt.Sprint(first.Email.CC) != "[boss@customer.example]" {
		t.Fatalf("email.cc = %+v, want only boss (support address, hosted address and reply alias are ours)", first.Email)
	}
}

func TestACopiedPersonsReplyAllThreadsAndKeepsTheContact(t *testing.T) {
	d := newCCDesk(t)
	var sent ccMessage
	reply := d.reply(t, []string{"boss@customer.example"})
	if err := json.Unmarshal([]byte(reply.Body), &sent); err != nil {
		t.Fatal(err)
	}
	ourID := queryString(t, d.pool, `SELECT email_message_id FROM messages WHERE id=$1`, sent.ID)
	result := inboundOK(t, d.api, d.secret, mail([]string{
		"From: Boss <boss@customer.example>", "To: support@cc.example.com, ada@customer.example", "Subject: Re: Invoice",
		"Message-ID: <boss-reply-all@customer.example>", "In-Reply-To: <" + ourID + ">", "References: <cc-start@customer.example> <" + ourID + ">",
	}, "Approved from my side."))
	if result.Status != "threaded" || result.ConversationID == nil || *result.ConversationID != d.conversation {
		t.Fatalf("reply-all must thread into the conversation: %+v", result)
	}
	if got := queryString(t, d.pool, `SELECT ct.email FROM conversations c JOIN contacts ct ON ct.id=c.contact_id WHERE c.id=$1`, d.conversation); got != d.contact {
		t.Fatalf("the copied person became the contact: %q", got)
	}
	if got := len(conversationList(t, d.api, d.admin)); got != 1 {
		t.Fatalf("conversations = %d, want 1", got)
	}
}
