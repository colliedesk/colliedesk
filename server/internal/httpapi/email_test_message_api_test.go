package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

type testMessageView struct {
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	SentTo         string `json:"sent_to"`
}

func sendTestMessage(t *testing.T, api http.Handler, token, path string) (*responseView, testMessageView) {
	t.Helper()
	response := request(t, api, http.MethodPost, path, nil, token, nil)
	view := testMessageView{}
	if response.Code == http.StatusCreated {
		decode(t, response, &view)
	}
	return &responseView{Code: response.Code, Body: response.Body.String()}, view
}

func (f limitsFixture) emailChannelID(t *testing.T, workspace string) string {
	t.Helper()
	return queryString(t, f.pool, `SELECT id::text FROM channels WHERE workspace_id=$1 AND type='email' ORDER BY is_default DESC, created_at LIMIT 1`, workspace)
}

func TestTestMessageOpensAConversationFromTheAgentThatTheTrialMayAnswer(t *testing.T) {
	f := newLimitsFixture(t, true)
	got, sent := sendTestMessage(t, f.api, f.adminA, "/api/v1/channels/email/test-message")
	if got.Code != http.StatusCreated {
		t.Fatalf("test message: status=%d body=%s, want 201", got.Code, got.Body)
	}
	if sent.SentTo != "admin@limits-alpha.example.com" || sent.ConversationID == "" || sent.MessageID == "" {
		t.Fatalf("test message result = %+v, want the conversation from admin@limits-alpha.example.com", sent)
	}
	if contact := queryString(t, f.pool, `SELECT k.email FROM conversations c JOIN contacts k ON k.workspace_id=c.workspace_id AND k.id=c.contact_id
		WHERE c.workspace_id=$1 AND c.id=$2`, f.workspaceA, sent.ConversationID); contact != "admin@limits-alpha.example.com" {
		t.Fatalf("contact of the test conversation = %q, want the agent's own address", contact)
	}
	if kind := queryString(t, f.pool, `SELECT kind FROM messages WHERE workspace_id=$1 AND id=$2`, f.workspaceA, sent.MessageID); kind != "incoming" {
		t.Fatalf("test message kind = %q, want incoming like real mail", kind)
	}
	f.wantReplyQueued(t, sent.ConversationID, "reply to the agent's own test message on a trial")
	if status := queryString(t, f.pool, `SELECT status FROM channel_outbox WHERE workspace_id=$1`, f.workspaceA); status != "queued" {
		t.Fatalf("outbox status = %q, want queued for delivery", status)
	}
}

func TestTheTestMessageIsNotMarkedAsAnUnverifiedSender(t *testing.T) {
	f := newLimitsFixture(t, true)
	_, sent := sendTestMessage(t, f.api, f.adminA, "/api/v1/channels/email/test-message")
	if got := queryString(t, f.pool, `SELECT email_meta->>'authentication' FROM messages WHERE workspace_id=$1 AND id=$2`, f.workspaceA, sent.MessageID); got != "pass" {
		t.Fatalf("authentication of the test message = %q, want pass: the product made it for the signed-in agent", got)
	}
}

func TestTrialAnswerToTheAgentsOwnTestMessageStillRefusesAnOutsideCc(t *testing.T) {
	f := newLimitsFixture(t, true)
	_, sent := sendTestMessage(t, f.api, f.adminA, "/api/v1/channels/email/test-message")
	got := f.replyCopying(t, sent.ConversationID, "stranger@outside.example")
	wantStatus(t, got, http.StatusForbidden, restrictedCode, "Cc to an outside address on the test conversation")
	if f.outboxCount(t, f.workspaceA) != "0" {
		t.Fatal("a refused reply must leave no outbox row")
	}
}

// An invite link proves nothing about its address: the admin who made it can open it. Only an agent who
// signed in through a pairing link (the server operator's, or a verified Cloud identity's) is a proven
// address the trial may write to without the agent having written first.
func TestTrialAnswersOnlyAnAgentWhoseAddressIsProven(t *testing.T) {
	f := newLimitsFixture(t, true)
	invited := joinAs(t, f, "someone@elsewhere.example", "agent")
	got, sent := sendTestMessage(t, f.api, invited, "/api/v1/channels/email/test-message")
	if got.Code != http.StatusCreated {
		t.Fatalf("test message by an invited agent: status=%d body=%s", got.Code, got.Body)
	}
	wantStatus(t, f.reply(t, f.adminA, sent.ConversationID), http.StatusForbidden, restrictedCode, "reply to an unproven agent address")
}

func TestTestMessageIsLimitedPerWorkspace(t *testing.T) {
	f := newLimitsFixture(t, true)
	for attempt := 1; attempt <= 5; attempt++ {
		if got, _ := sendTestMessage(t, f.api, f.adminA, "/api/v1/channels/email/test-message"); got.Code != http.StatusCreated {
			t.Fatalf("test message %d: status=%d body=%s, want 201", attempt, got.Code, got.Body)
		}
	}
	response := request(t, f.api, http.MethodPost, "/api/v1/channels/email/test-message", nil, f.adminA, nil)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" || !strings.Contains(response.Body.String(), `"rate_limited"`) {
		t.Fatalf("sixth test message in an hour: status=%d retry-after=%q body=%s, want 429 rate_limited", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
	if got, _ := sendTestMessage(t, f.api, f.adminB, "/api/v1/channels/email/test-message"); got.Code != http.StatusCreated {
		t.Fatalf("another workspace keeps its own allowance: status=%d body=%s", got.Code, got.Body)
	}
}

func TestTestMessageStaysInsideItsWorkspace(t *testing.T) {
	f := newLimitsFixture(t, true)
	channelA, channelB := f.emailChannelID(t, f.workspaceA), f.emailChannelID(t, f.workspaceB)
	before := queryString(t, f.pool, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`, f.workspaceB)
	got, _ := sendTestMessage(t, f.api, f.adminA, "/api/v1/channels/"+channelB+"/email/test-message")
	if got.Code != http.StatusNotFound {
		t.Fatalf("another workspace's channel: status=%d body=%s, want 404", got.Code, got.Body)
	}
	if after := queryString(t, f.pool, `SELECT count(*)::text FROM conversations WHERE workspace_id=$1`, f.workspaceB); after != before {
		t.Fatalf("workspace B conversations %s -> %s: nothing may be written there", before, after)
	}
	got, sent := sendTestMessage(t, f.api, f.adminA, "/api/v1/channels/"+channelA+"/email/test-message")
	if got.Code != http.StatusCreated {
		t.Fatalf("own channel by id: status=%d body=%s", got.Code, got.Body)
	}
	if channel := queryString(t, f.pool, `SELECT channel_id::text FROM conversations WHERE workspace_id=$1 AND id=$2`, f.workspaceA, sent.ConversationID); channel != channelA {
		t.Fatalf("test conversation channel = %q, want %q", channel, channelA)
	}
}

func TestTestMessageNeedsAConnectedEmailChannel(t *testing.T) {
	api, service, _, _ := testAPIOnPool(t, testPoolTraced(t, nil), nil)
	admin := createSession(t, api, service, "No Email", "admin@no-email.example.com")
	got, _ := sendTestMessage(t, api, admin, "/api/v1/channels/email/test-message")
	if got.Code != http.StatusNotFound {
		t.Fatalf("no email channel: status=%d body=%s, want 404", got.Code, got.Body)
	}
}

func TestATestMessageFromTheSupportAddressItselfSaysWhyAndCostsNothing(t *testing.T) {
	api, service, _, _ := testAPIOnPool(t, testPoolTraced(t, nil), nil)
	admin := createSession(t, api, service, "Own Address", "support@own-address.example.com")
	connectEmail(t, api, admin, "support@own-address.example.com")
	for attempt := 0; attempt < 6; attempt++ {
		got, _ := sendTestMessage(t, api, admin, "/api/v1/channels/email/test-message")
		if got.Code != http.StatusConflict || !strings.Contains(got.Body, `"own_address"`) {
			t.Fatalf("attempt %d from the support address: status=%d body=%s, want 409 own_address", attempt, got.Code, got.Body)
		}
	}
}
