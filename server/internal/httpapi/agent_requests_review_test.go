package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// seedIncomingWithCC stores customer mail on the conversation that copied cc, so cc is a participant.
func seedIncomingWithCC(t *testing.T, desk requestDesk, from string, cc ...string) {
	t.Helper()
	meta, _ := json.Marshal(map[string]any{"from": from, "cc": cc, "authentication": "pass", "via": "platform"})
	if _, err := desk.pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body,email_meta)
		SELECT workspace_id,id,'incoming','Help please',$2 FROM conversations WHERE id=$1`, desk.conversation, meta); err != nil {
		t.Fatal(err)
	}
}

type sentMessage struct {
	Kind           string   `json:"kind"`
	Body           string   `json:"body"`
	CC             []string `json:"cc"`
	AgentRequestID *string  `json:"agent_request_id"`
	AgentToken     *struct {
		Label string `json:"label"`
	} `json:"agent_token"`
}

func outgoing(t *testing.T, desk requestDesk) []sentMessage {
	t.Helper()
	var timeline struct {
		Items []sentMessage `json:"items"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/conversations/"+desk.conversation+"/messages", nil, desk.admin, nil), &timeline)
	var sent []sentMessage
	for _, item := range timeline.Items {
		if item.Kind == "outgoing" {
			sent = append(sent, item)
		}
	}
	return sent
}

func TestReplyDraftShowsEveryRecipientAndTheApproverMayOnlyRemoveCopies(t *testing.T) {
	desk := newRequestDesk(t, "Copy Desk")
	seedIncomingWithCC(t, desk, "ada@copy-desk.example.com", "boss@cust.example.com")
	read := issueToken(t, desk.api, desk.admin, "reader", "read").Token
	messagesPath := "/api/v1/conversations/" + desk.conversation + "/messages"

	// A token below send may copy only people already on the conversation.
	response := request(t, desk.api, http.MethodPost, messagesPath, map[string]any{"kind": "outgoing", "body": "Hi", "cc": []string{"x@evil.example.com"}}, read, nil)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "not on this conversation") {
		t.Fatalf("draft copying a stranger: status=%d body=%s, want 400 naming the address", response.Code, response.Body.String())
	}
	if items := listRequests(t, desk.api, desk.admin, ""); len(items) != 0 {
		t.Fatalf("a refused draft was stored: %+v", items)
	}

	created := accepted(t, desk.api, http.MethodPost, messagesPath, map[string]any{"kind": "outgoing", "body": "Hi Ada, fixed.", "cc": []string{"boss@cust.example.com"}}, read)
	for _, recipient := range []string{"ada@copy-desk.example.com", "boss@cust.example.com"} {
		if !strings.Contains(created.Summary, recipient) {
			t.Errorf("summary %q does not name recipient %s", created.Summary, recipient)
		}
	}
	// The approval call can remove a copy but never add one.
	if response := approve(t, desk.api, desk.admin, created.RequestID, map[string]any{"cc": []string{"boss@cust.example.com", "new@cust.example.com"}}); response.Code != http.StatusBadRequest {
		t.Fatalf("approve adding a copy: status=%d body=%s, want 400", response.Code, response.Body.String())
	}
	if got := getRequest(t, desk.api, desk.admin, created.RequestID); got.Status != "pending" {
		t.Fatalf("a refused approval changed the request: %+v", got)
	}
	if response := approve(t, desk.api, desk.admin, created.RequestID, map[string]any{"cc": []string{}}); response.Code != http.StatusOK {
		t.Fatalf("approve without the copy: status=%d body=%s", response.Code, response.Body.String())
	}
	sent := outgoing(t, desk)
	if len(sent) != 1 || len(sent[0].CC) != 0 || sent[0].Body != "Hi Ada, fixed." {
		t.Fatalf("sent = %+v, want the reply without the removed copy", sent)
	}
	// The words are still the agent's, so the reply still names the token that wrote them.
	if sent[0].AgentToken == nil || sent[0].AgentToken.Label != "reader" {
		t.Errorf("an unedited text must stay attributed to its token: %+v", sent[0])
	}
}

func TestASendTokenMayDraftACopyToANewAddress(t *testing.T) {
	desk := newRequestDesk(t, "New Copy Desk")
	send := issueToken(t, desk.api, desk.admin, "sender", "send").Token
	created := accepted(t, desk.api, http.MethodPost, "/api/v1/conversations/"+desk.conversation+"/messages",
		map[string]any{"kind": "outgoing", "body": "Looping in billing.", "cc": []string{"billing@cust.example.com"}, "draft": true}, send)
	if !strings.Contains(created.Summary, "billing@cust.example.com") {
		t.Errorf("summary %q does not name the copy", created.Summary)
	}
}

// cancelOnClaim cancels the approver's request the moment the request is claimed, as a closed tab does.
type cancelOnClaim struct {
	mu     sync.Mutex
	cancel context.CancelFunc
}

func (c *cancelOnClaim) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if strings.Contains(data.SQL, "SET status='approved'") {
		c.mu.Lock()
		if c.cancel != nil {
			c.cancel()
		}
		c.mu.Unlock()
	}
	return ctx
}

func (c *cancelOnClaim) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestApprovalFinishesWhenTheApproverLeavesMidway(t *testing.T) {
	tracer := &cancelOnClaim{}
	api, service, _, _ := testAPIOnPool(t, testPoolTraced(t, tracer), nil)
	admin := createSession(t, api, service, "Leaving Desk", "admin@leaving-desk.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@leaving-desk.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer jam"})
	read := issueToken(t, api, admin, "reader", "read").Token
	created := accepted(t, api, http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"status": "pending"}, read)

	ctx, cancel := context.WithCancel(context.Background())
	tracer.mu.Lock()
	tracer.cancel = cancel
	tracer.mu.Unlock()
	serve(api, newRequest(t, http.MethodPost, "/api/v1/agent-requests/"+created.RequestID+"/approve", nil, admin, nil).WithContext(ctx))
	if ctx.Err() == nil {
		t.Fatal("the approver's request was never cancelled: the test did not reach the claim")
	}
	got := getRequest(t, api, admin, created.RequestID)
	if got.Status != "approved" || got.ResultStatus == nil || *got.ResultStatus != http.StatusOK {
		t.Fatalf("request after the approver left = status %s result %v, want approved with the call's result", got.Status, got.ResultStatus)
	}
	if status := conversationStatus(t, api, admin, conversation); status != "pending" {
		t.Fatalf("conversation status = %q, want the approved change", status)
	}
}

func TestATokenFilesAtMost100RequestsAnHour(t *testing.T) {
	desk := newRequestDesk(t, "Busy Desk")
	issued := issueToken(t, desk.api, desk.admin, "busy", "read")
	// 100 requests filed in the last hour, all decided already, so the pending cap is not what refuses.
	if _, err := desk.pool.Exec(context.Background(), `INSERT INTO agent_requests(workspace_id,token_id,token_label,token_scope,kind,method,route,path,summary,status,decided_at,created_at,expires_at)
		SELECT s.workspace_id,s.id,s.label,s.scope,'action','PATCH','/api/v1/tags/{id}','/api/v1/tags/x','Rename a tag','cancelled',$2::timestamptz,$2::timestamptz - n * interval '30 seconds',$2::timestamptz + interval '7 days'
		FROM sessions s, generate_series(1,100) n WHERE s.id=$1`, issued.Session.ID, desk.clock.Now()); err != nil {
		t.Fatal(err)
	}
	response := request(t, desk.api, http.MethodPatch, "/api/v1/conversations/"+desk.conversation, map[string]any{"status": "closed"}, issued.Token, nil)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("101st request in an hour: status=%d body=%s, want 429", response.Code, response.Body.String())
	}
	if wait, err := strconv.Atoi(response.Header().Get("Retry-After")); err != nil || wait <= 0 || wait > 3600 {
		t.Errorf("Retry-After = %q, want the seconds until the oldest of the hour leaves the window", response.Header().Get("Retry-After"))
	}
	desk.clock.set(desk.clock.Now().Add(time.Hour))
	accepted(t, desk.api, http.MethodPatch, "/api/v1/conversations/"+desk.conversation, map[string]any{"status": "closed"}, issued.Token)
}

func TestDecidedRequestsLoseTheirCallAfter90DaysAndKeepTheRecord(t *testing.T) {
	desk := newRequestDesk(t, "Prune Desk")
	read := issueToken(t, desk.api, desk.admin, "reader", "read").Token
	old := accepted(t, desk.api, http.MethodPatch, "/api/v1/conversations/"+desk.conversation, map[string]any{"status": "closed"}, read)
	recent := accepted(t, desk.api, http.MethodPatch, "/api/v1/conversations/"+desk.conversation, map[string]any{"priority": "high"}, read)
	for _, id := range []string{old.RequestID, recent.RequestID} {
		if response := request(t, desk.api, http.MethodPost, "/api/v1/agent-requests/"+id+"/reject", nil, desk.admin, nil); response.Code != http.StatusOK {
			t.Fatalf("reject status=%d", response.Code)
		}
	}
	// Decided 91 and 89 days ago: only the first is past the 90 days.
	for id, days := range map[string]int{old.RequestID: 91, recent.RequestID: 89} {
		if _, err := desk.pool.Exec(context.Background(), `UPDATE agent_requests SET decided_at=$2::timestamptz - $3 * interval '1 day' WHERE id=$1`, id, desk.clock.Now(), days); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.New(desk.pool).WithClock(desk.clock.Now).PruneAgentRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	pruned := getRequest(t, desk.api, desk.admin, old.RequestID)
	if (len(pruned.Body) > 0 && string(pruned.Body) != "null") || pruned.Status != "rejected" || pruned.Summary == "" || pruned.DecidedBy == nil || pruned.Route == "" {
		t.Errorf("a request decided 91 days ago = %+v, want the audit row without the call body", pruned)
	}
	if kept := getRequest(t, desk.api, desk.admin, recent.RequestID); !strings.Contains(string(kept.Body), "high") {
		t.Errorf("a request decided 89 days ago lost its body: %+v", kept)
	}
}

func TestATokenOfALockedWorkspaceFilesNoRequest(t *testing.T) {
	f := newLimitsFixture(t, true)
	read := issueToken(t, f.api, f.adminA, "reader", "read").Token
	f.clock.set(trialEnd)
	response := request(t, f.api, http.MethodPost, "/api/v1/tags", map[string]any{"name": "late"}, read, nil)
	if response.Code != http.StatusPaymentRequired || !strings.Contains(response.Body.String(), "workspace_locked") {
		t.Fatalf("locked workspace: status=%d body=%s, want 402 workspace_locked", response.Code, response.Body.String())
	}
	var count int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_requests`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("agent requests after a locked call = %d (%v), want 0", count, err)
	}
}

func TestExportKeepsAgentRequestsAndTheMessagesTheySent(t *testing.T) {
	desk := newRequestDesk(t, "Export Requests Desk")
	draft := issueToken(t, desk.api, desk.admin, "drafter", "draft").Token
	created := accepted(t, desk.api, http.MethodPost, "/api/v1/conversations/"+desk.conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Exported draft"}, draft)
	if response := approve(t, desk.api, desk.admin, created.RequestID, nil); response.Code != http.StatusOK {
		t.Fatalf("approve status=%d", response.Code)
	}
	response := request(t, desk.api, http.MethodGet, "/api/v1/export", nil, desk.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}
	entries := unzip(t, response.Body.Bytes())
	requests := ndjsonLines(t, entries["agent-requests.ndjson"])
	if len(requests) != 1 || requests[0]["id"] != created.RequestID || requests[0]["status"] != "approved" || requests[0]["token_label"] != "drafter" {
		t.Fatalf("agent-requests.ndjson = %v", requests)
	}
	if _, leaked := requests[0]["result"]; leaked {
		t.Errorf("an exported request carries result: %v", requests[0])
	}
	var linked bool
	for _, conversation := range ndjsonLines(t, entries["conversations.ndjson"]) {
		for _, message := range conversation["messages"].([]any) {
			if message.(map[string]any)["agent_request_id"] == created.RequestID {
				linked = true
			}
		}
	}
	if !linked {
		t.Error("no exported message names the request that sent it")
	}
	var manifest struct {
		Counts map[string]int `json:"counts"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil || manifest.Counts["agent_requests"] != 1 {
		t.Errorf("manifest counts = %v (%v), want agent_requests 1", manifest.Counts, err)
	}
}
