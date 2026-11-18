package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// milestone is one row of a workspace's lifecycle log as the tests read it.
type milestone struct {
	Kind   string
	Source string
	At     time.Time
	Detail map[string]any
}

func milestonesOf(t *testing.T, pool *pgxpool.Pool, workspace string) []milestone {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT kind, source, at, detail FROM workspace_events WHERE workspace_id=$1 ORDER BY id`, workspace)
	if err != nil {
		t.Fatal(err)
	}
	list, err := pgx.CollectRows(rows, pgx.RowToStructByPos[milestone])
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func kindsOf(list []milestone) string {
	kinds := make([]string, 0, len(list))
	for _, item := range list {
		kinds = append(kinds, item.Kind)
	}
	return strings.Join(kinds, ",")
}

func onlyKind(list []milestone, kind string) []milestone {
	var found []milestone
	for _, item := range list {
		if item.Kind == kind {
			found = append(found, item)
		}
	}
	return found
}

// cookieCall sends a request as the browser of a signed-in person: the session cookie from the app's origin.
func cookieCall(t *testing.T, api http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := newRequest(t, method, path, body, "", cookie)
	req.Header.Set("Origin", testOrigin)
	return serve(api, req)
}

func TestASignUpAndItsOwnTestMessageRecordOnlySignedUp(t *testing.T) {
	storage, err := attachments.NewLocalDisk(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := newSignupFixture(t, func(config *httpapi.Config) { config.Attachments = storage })
	response := f.signUp(t, "grace@example.com", "Hopper Help", defaultPeer)
	wantResponse(t, response, http.StatusCreated, "")
	cookie := sessionCookie(t, response)
	workspace := queryString(t, f.pool, `SELECT id::text FROM workspaces WHERE slug='hopper-help'`)

	sent := cookieCall(t, f.api, http.MethodPost, "/api/v1/channels/email/test-message", nil, cookie)
	if sent.Code != http.StatusCreated {
		t.Fatalf("test message: status=%d body=%s", sent.Code, sent.Body.String())
	}
	var test testMessageView
	decode(t, sent, &test)
	answered := cookieCall(t, f.api, http.MethodPost, "/api/v1/conversations/"+test.ConversationID+"/messages",
		map[string]any{"kind": "outgoing", "body": "It works"}, cookie)
	if answered.Code != http.StatusCreated {
		t.Fatalf("reply to the test message: status=%d body=%s", answered.Code, answered.Body.String())
	}

	got := milestonesOf(t, f.pool, workspace)
	if kindsOf(got) != "signed_up" || got[0].Source != "app" {
		t.Fatalf("milestones = %+v, want only signed_up from the app: the test message and its answer involve no customer", got)
	}
	if same := queryString(t, f.pool, `SELECT (e.at = w.created_at)::text FROM workspace_events e JOIN workspaces w ON w.id=e.workspace_id
		WHERE e.workspace_id=$1 AND e.kind='signed_up'`, workspace); same != "true" {
		t.Fatal("signed_up must carry the time the workspace row was created")
	}
}

func TestTheFirstLiveCustomerMessageIsRecordedOnceAndKeepsItsTime(t *testing.T) {
	f := newLimitsFixture(t, true)
	// Mail from the workspace's own agent and Gmail's forwarding confirmation are no customer.
	f.receiveViaWorkspaceSecret(t, "admin@limits-alpha.example.com")
	inboundOK(t, f.api, f.secretA, syntheticGmailConfirmation("support@limits-alpha.example.com", "confirm-1", googleAuth))
	if got := onlyKind(milestonesOf(t, f.pool, f.workspaceA), "first_customer_message"); len(got) != 0 {
		t.Fatalf("first_customer_message before any customer wrote: %+v", got)
	}

	first := f.receive(t, false, 1, "Order missing")
	second := f.receive(t, false, 2, "Another question")
	if first.MessageID == nil || second.MessageID == nil {
		t.Fatalf("customer mail not stored: %+v %+v", first, second)
	}
	got := onlyKind(milestonesOf(t, f.pool, f.workspaceA), "first_customer_message")
	if len(got) != 1 || got[0].Detail["message_id"] != *first.MessageID || got[0].Source != "app" {
		t.Fatalf("first_customer_message = %+v, want one naming the first message %s", got, *first.MessageID)
	}
	if same := queryString(t, f.pool, `SELECT (e.at = m.created_at)::text FROM workspace_events e
		JOIN messages m ON m.workspace_id=e.workspace_id AND m.id=$2::uuid
		WHERE e.workspace_id=$1 AND e.kind='first_customer_message'`, f.workspaceA, *first.MessageID); same != "true" {
		t.Fatal("first_customer_message must keep the time of the first message after a second one arrived")
	}
	if got := onlyKind(milestonesOf(t, f.pool, f.workspaceB), "first_customer_message"); len(got) != 0 {
		t.Fatalf("workspace B got A's customer message: %+v", got)
	}
}

func TestAPrivateNoteIsNoFirstReplyAndASentReplyIs(t *testing.T) {
	f := newLimitsFixture(t, true)
	conversation := *f.receive(t, false, 1, "Order missing").ConversationID
	note := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages",
		map[string]any{"kind": "note", "body": "Checking the warehouse"}, f.adminA, nil)
	if note.Code != http.StatusCreated {
		t.Fatalf("note: status=%d body=%s", note.Code, note.Body.String())
	}
	if got := onlyKind(milestonesOf(t, f.pool, f.workspaceA), "first_reply"); len(got) != 0 {
		t.Fatalf("a private note made a first_reply: %+v", got)
	}
	reply := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages",
		map[string]any{"kind": "outgoing", "body": "It ships today"}, f.adminA, nil)
	if reply.Code != http.StatusCreated {
		t.Fatalf("reply: status=%d body=%s", reply.Code, reply.Body.String())
	}
	var sent struct {
		ID string `json:"id"`
	}
	decode(t, reply, &sent)
	if got := f.reply(t, f.adminA, conversation); got.Code != http.StatusCreated {
		t.Fatalf("second reply: status=%d body=%s", got.Code, got.Body)
	}
	got := onlyKind(milestonesOf(t, f.pool, f.workspaceA), "first_reply")
	if len(got) != 1 || got[0].Detail["message_id"] != sent.ID {
		t.Fatalf("first_reply = %+v, want one naming the first reply %s", got, sent.ID)
	}
	if got := onlyKind(milestonesOf(t, f.pool, f.workspaceB), "first_reply"); len(got) != 0 {
		t.Fatalf("workspace B got A's reply: %+v", got)
	}
}
