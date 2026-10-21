package httpapi_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/auth"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
	"github.com/jackc/pgx/v5/pgxpool"
)

type acceptedView struct {
	RequestID string `json:"request_id"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Summary   string `json:"summary"`
	ExpiresAt string `json:"expires_at"`
}

type agentRequestView struct {
	ID             string          `json:"id"`
	Kind           string          `json:"kind"`
	Status         string          `json:"status"`
	Method         string          `json:"method"`
	Route          string          `json:"route"`
	Path           string          `json:"path"`
	Body           json.RawMessage `json:"body"`
	Summary        string          `json:"summary"`
	ConversationID *string         `json:"conversation_id"`
	EditedBody     json.RawMessage `json:"edited_body"`
	ApprovableBy   string          `json:"approvable_by"`
	RequestedBy    struct {
		TokenLabel   string  `json:"token_label"`
		TokenScope   string  `json:"token_scope"`
		TokenRevoked bool    `json:"token_revoked"`
		AgentName    *string `json:"agent_name"`
	} `json:"requested_by"`
	DecidedBy *struct {
		Name string `json:"name"`
	} `json:"decided_by"`
	Reason       *string         `json:"reason"`
	ResultStatus *int            `json:"result_status"`
	Result       json.RawMessage `json:"result"`
	ResultNote   *string         `json:"result_note"`
	CreatedAt    time.Time       `json:"created_at"`
	ExpiresAt    time.Time       `json:"expires_at"`
}

type approvedView struct {
	Request agentRequestView `json:"request"`
	Result  json.RawMessage  `json:"result"`
}

// accepted asserts the 202 an agent token gets instead of a refusal, checks it against the contract
// and returns it.
func accepted(t *testing.T, api http.Handler, method, path string, body any, token string) acceptedView {
	t.Helper()
	req := newRequest(t, method, path, body, token, nil)
	response := serve(api, req)
	if response.Code != http.StatusAccepted {
		t.Fatalf("%s %s status=%d body=%s, want 202 with an agent request", method, path, response.Code, response.Body.String())
	}
	validateContractResponse(t, newRequest(t, method, path, body, token, nil), response)
	var view acceptedView
	decode(t, response, &view)
	if view.RequestID == "" || view.Status != "pending" || view.Summary == "" || view.ExpiresAt == "" {
		t.Fatalf("202 body = %+v", view)
	}
	return view
}

func getRequest(t *testing.T, api http.Handler, token, id string) agentRequestView {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/agent-requests/"+id, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET request %s status=%d body=%s", id, response.Code, response.Body.String())
	}
	var view agentRequestView
	decode(t, response, &view)
	return view
}

func listRequests(t *testing.T, api http.Handler, token, query string) []agentRequestView {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/agent-requests"+query, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list requests status=%d body=%s", response.Code, response.Body.String())
	}
	var page struct {
		Items []agentRequestView `json:"items"`
	}
	decode(t, response, &page)
	return page.Items
}

func approve(t *testing.T, api http.Handler, token, id string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, api, http.MethodPost, "/api/v1/agent-requests/"+id+"/approve", body, token, nil)
}

// joinAsAgent invites email with the agent role and returns the session of the person who redeems it.
func joinAsAgent(t *testing.T, api http.Handler, admin, email string) string {
	t.Helper()
	_, link := createContractInvite(t, api, admin, email, "agent")
	response := request(t, api, http.MethodPost, "/api/v1/auth/redeem", map[string]any{"token": linkToken(t, link)}, "", nil)
	var session struct {
		Token string `json:"token"`
	}
	decode(t, response, &session)
	if session.Token == "" {
		t.Fatalf("redeem agent invite status=%d body=%s", response.Code, response.Body.String())
	}
	return session.Token
}

type requestDesk struct {
	api                   http.Handler
	admin                 string
	conversation, contact string
	clock                 *testClock
	service               *auth.Service
	pool                  *pgxpool.Pool
}

func newRequestDesk(t *testing.T, name string) requestDesk {
	t.Helper()
	clock := &testClock{now: time.Date(2026, time.October, 17, 9, 0, 0, 0, time.UTC)}
	api, service, pool, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Now = clock.Now })
	admin := createSession(t, api, service, name, "admin@"+strings.ToLower(strings.ReplaceAll(name, " ", "-"))+".example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@" + strings.ToLower(strings.ReplaceAll(name, " ", "-")) + ".example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer jam"})
	return requestDesk{api: api, admin: admin, conversation: conversation, contact: contact, clock: clock, service: service, pool: pool}
}

func conversationStatus(t *testing.T, api http.Handler, token, id string) string {
	t.Helper()
	var conversation struct {
		Status string `json:"status"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+id, nil, token, nil), &conversation)
	return conversation.Status
}

func TestReadTokenWriteBecomesARequestAndChangesNothingUntilApproved(t *testing.T) {
	desk := newRequestDesk(t, "Request Desk")
	read := issueToken(t, desk.api, desk.admin, "Claude triage", "read").Token
	path := "/api/v1/conversations/" + desk.conversation

	created := accepted(t, desk.api, http.MethodPatch, path, map[string]any{"status": "pending"}, read)
	if created.Kind != "action" || !strings.Contains(created.Summary, "pending") {
		t.Errorf("request = %+v, want an action whose summary names the change", created)
	}
	if got := conversationStatus(t, desk.api, desk.admin, desk.conversation); got != "open" {
		t.Fatalf("status before approval = %q, want open: a request must never change anything by itself", got)
	}

	mine := listRequests(t, desk.api, read, "")
	if len(mine) != 1 || mine[0].ID != created.RequestID || mine[0].Method != "PATCH" || mine[0].Path != path ||
		mine[0].Route != "/api/v1/conversations/{id}" || mine[0].RequestedBy.TokenLabel != "Claude triage" || mine[0].RequestedBy.TokenScope != "read" {
		t.Fatalf("the token's own list = %+v", mine)
	}
	// The list every tab polls stays small: the call's body comes with one request, or with detail=full.
	if len(mine[0].Body) > 0 && string(mine[0].Body) != "null" {
		t.Errorf("list carries the body %s, want only the summary fields", mine[0].Body)
	}
	if full := listRequests(t, desk.api, read, "?detail=full"); len(full) != 1 || !strings.Contains(string(full[0].Body), `"pending"`) {
		t.Errorf("detail=full = %+v, want the body", full)
	}
	if one := getRequest(t, desk.api, read, created.RequestID); !strings.Contains(string(one.Body), `"pending"`) {
		t.Errorf("stored body = %s, want the exact call", one.Body)
	}
	if mine[0].ConversationID == nil || *mine[0].ConversationID != desk.conversation {
		t.Errorf("conversation_id = %v, want %s", mine[0].ConversationID, desk.conversation)
	}

	response := approve(t, desk.api, desk.admin, created.RequestID, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", response.Code, response.Body.String())
	}
	var approved approvedView
	decode(t, response, &approved)
	if approved.Request.Status != "approved" || approved.Request.DecidedBy == nil || approved.Request.DecidedBy.Name == "" ||
		approved.Request.ResultStatus == nil || *approved.Request.ResultStatus != http.StatusOK {
		t.Fatalf("approved request = %+v", approved.Request)
	}
	if got := conversationStatus(t, desk.api, desk.admin, desk.conversation); got != "pending" {
		t.Fatalf("status after approval = %q, want pending", got)
	}
	// The token reads the outcome of its own request.
	if got := getRequest(t, desk.api, read, created.RequestID); got.Status != "approved" || got.DecidedBy == nil || len(got.Result) == 0 {
		t.Errorf("token's view of the approved request = %+v", got)
	}
}

func TestReplyDraftIsSentOnlyWhenAPersonApprovesItWithTheirEdit(t *testing.T) {
	desk := newRequestDesk(t, "Draft Desk")
	draft := issueToken(t, desk.api, desk.admin, "Claude drafts", "draft").Token
	messagesPath := "/api/v1/conversations/" + desk.conversation + "/messages"

	// A note from a draft token is still written directly.
	if response := request(t, desk.api, http.MethodPost, messagesPath, map[string]any{"kind": "note", "body": "Checked the logs"}, draft, nil); response.Code != http.StatusCreated {
		t.Fatalf("draft note status=%d body=%s", response.Code, response.Body.String())
	}
	created := accepted(t, desk.api, http.MethodPost, messagesPath, map[string]any{"kind": "outgoing", "body": "Hello Ada, try turning it off."}, draft)
	if created.Kind != "reply_draft" {
		t.Fatalf("kind = %q, want reply_draft", created.Kind)
	}
	var timeline struct {
		Items []struct {
			Kind           string  `json:"kind"`
			Body           string  `json:"body"`
			AuthorAgentID  *string `json:"author_agent_id"`
			AgentRequestID *string `json:"agent_request_id"`
			AgentToken     *struct {
				Label string `json:"label"`
			} `json:"agent_token"`
		} `json:"items"`
	}
	decode(t, request(t, desk.api, http.MethodGet, messagesPath, nil, desk.admin, nil), &timeline)
	if len(timeline.Items) != 1 {
		t.Fatalf("a draft must not reach the timeline before Send; items = %+v", timeline.Items)
	}

	// Only a reply draft takes an edited body; Send records the person who pressed it.
	response := approve(t, desk.api, desk.admin, created.RequestID, map[string]any{"body": "Hello Ada, please restart the printer."})
	if response.Code != http.StatusOK {
		t.Fatalf("send draft status=%d body=%s", response.Code, response.Body.String())
	}
	decode(t, request(t, desk.api, http.MethodGet, messagesPath, nil, desk.admin, nil), &timeline)
	if len(timeline.Items) != 2 {
		t.Fatalf("timeline after Send = %+v", timeline.Items)
	}
	sent := timeline.Items[1]
	if sent.Kind != "outgoing" || sent.Body != "Hello Ada, please restart the printer." {
		t.Errorf("sent reply = %+v, want the edited body", sent)
	}
	// The words are the approver's now: the reply names the request, not the token as its writer.
	if sent.AgentRequestID == nil || *sent.AgentRequestID != created.RequestID || sent.AgentToken != nil {
		t.Errorf("an edited reply must name the request and not the token as its writer: %+v", sent)
	}
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/auth/me", nil, desk.admin, nil), &me)
	if sent.AuthorAgentID == nil || *sent.AuthorAgentID != me.Agent.ID {
		t.Errorf("sent reply must be written by the person who approved it (%s): %+v", me.Agent.ID, sent)
	}
	stored := getRequest(t, desk.api, desk.admin, created.RequestID)
	if !strings.Contains(string(stored.Body), "try turning it off") || !strings.Contains(string(stored.EditedBody), "please restart") {
		t.Errorf("the request keeps the agent's draft and the edit apart; body = %s, edited_body = %s", stored.Body, stored.EditedBody)
	}

	// An action request takes no edited body.
	action := accepted(t, desk.api, http.MethodPatch, "/api/v1/conversations/"+desk.conversation, map[string]any{"priority": "high"},
		issueToken(t, desk.api, desk.admin, "reader", "read").Token)
	if response := approve(t, desk.api, desk.admin, action.RequestID, map[string]any{"body": "x"}); response.Code != http.StatusBadRequest {
		t.Errorf("edited body on an action: status=%d body=%s, want 400", response.Code, response.Body.String())
	}
}

func TestDiscardedDraftIsNeverSentAndCannotBeReplayed(t *testing.T) {
	desk := newRequestDesk(t, "Discard Desk")
	draft := issueToken(t, desk.api, desk.admin, "Claude drafts", "draft").Token
	messagesPath := "/api/v1/conversations/" + desk.conversation + "/messages"
	created := accepted(t, desk.api, http.MethodPost, messagesPath, map[string]any{"kind": "outgoing", "body": "Wrong answer"}, draft)

	response := request(t, desk.api, http.MethodPost, "/api/v1/agent-requests/"+created.RequestID+"/reject", map[string]any{"reason": "Not accurate"}, desk.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("reject status=%d body=%s", response.Code, response.Body.String())
	}
	var rejected agentRequestView
	decode(t, response, &rejected)
	if rejected.Status != "rejected" || rejected.Reason == nil || *rejected.Reason != "Not accurate" || rejected.DecidedBy == nil {
		t.Fatalf("rejected = %+v", rejected)
	}
	if response := approve(t, desk.api, desk.admin, created.RequestID, nil); response.Code != http.StatusConflict {
		t.Fatalf("approve after reject: status=%d body=%s, want 409", response.Code, response.Body.String())
	}
	var timeline struct {
		Items []struct{} `json:"items"`
	}
	decode(t, request(t, desk.api, http.MethodGet, messagesPath, nil, desk.admin, nil), &timeline)
	if len(timeline.Items) != 0 {
		t.Fatalf("a discarded draft reached the timeline: %d messages", len(timeline.Items))
	}
}

func TestCancelledOrExpiredRequestCannotBeApproved(t *testing.T) {
	desk := newRequestDesk(t, "Expiry Desk")
	read := issueToken(t, desk.api, desk.admin, "reader", "read").Token
	path := "/api/v1/conversations/" + desk.conversation

	cancelled := accepted(t, desk.api, http.MethodPatch, path, map[string]any{"status": "closed"}, read)
	response := request(t, desk.api, http.MethodPost, "/api/v1/agent-requests/"+cancelled.RequestID+"/cancel", nil, read, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("cancel by the token status=%d body=%s", response.Code, response.Body.String())
	}
	if response := approve(t, desk.api, desk.admin, cancelled.RequestID, nil); response.Code != http.StatusConflict {
		t.Errorf("approve after cancel: status=%d body=%s, want 409", response.Code, response.Body.String())
	}

	stale := accepted(t, desk.api, http.MethodPatch, path, map[string]any{"status": "resolved"}, read)
	if got, want := stale.ExpiresAt, "2026-10-24T09:00:00Z"; got != want {
		t.Errorf("expires_at = %s, want %s (seven days)", got, want)
	}
	desk.clock.set(time.Date(2026, time.October, 24, 9, 0, 1, 0, time.UTC))
	if got := getRequest(t, desk.api, desk.admin, stale.RequestID); got.Status != "expired" {
		t.Errorf("status after seven days = %q, want expired", got.Status)
	}
	if pending := listRequests(t, desk.api, desk.admin, "?status=pending"); len(pending) != 0 {
		t.Errorf("pending after expiry = %+v", pending)
	}
	if response := approve(t, desk.api, desk.admin, stale.RequestID, nil); response.Code != http.StatusConflict {
		t.Errorf("approve after expiry: status=%d body=%s, want 409", response.Code, response.Body.String())
	}
	if got := conversationStatus(t, desk.api, desk.admin, desk.conversation); got != "open" {
		t.Errorf("status = %q, want open: neither request may run", got)
	}
}

func TestDoubleApproveRunsTheCallOnce(t *testing.T) {
	desk := newRequestDesk(t, "Double Desk")
	draft := issueToken(t, desk.api, desk.admin, "drafter", "draft").Token
	messagesPath := "/api/v1/conversations/" + desk.conversation + "/messages"
	created := accepted(t, desk.api, http.MethodPost, messagesPath, map[string]any{"kind": "outgoing", "body": "Once only"}, draft)

	codes := make([]int, 4)
	var wait sync.WaitGroup
	for index := range codes {
		wait.Add(1)
		go func() {
			defer wait.Done()
			codes[index] = serve(desk.api, newRequest(t, http.MethodPost, "/api/v1/agent-requests/"+created.RequestID+"/approve", nil, desk.admin, nil)).Code
		}()
	}
	wait.Wait()
	ok, conflict := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		}
	}
	if ok != 1 || conflict != len(codes)-1 {
		t.Fatalf("approve codes = %v, want one 200 and the rest 409", codes)
	}
	var timeline struct {
		Items []struct{} `json:"items"`
	}
	decode(t, request(t, desk.api, http.MethodGet, messagesPath, nil, desk.admin, nil), &timeline)
	if len(timeline.Items) != 1 {
		t.Fatalf("messages after four clicks = %d, want 1", len(timeline.Items))
	}
}

func TestApproverWithoutPermissionIsRefusedAndTheRequestKeepsWaiting(t *testing.T) {
	desk := newRequestDesk(t, "Permission Desk")
	send := issueToken(t, desk.api, desk.admin, "sender", "send").Token
	agent := joinAsAgent(t, desk.api, desk.admin, "agent@permission-desk.example.com")

	created := accepted(t, desk.api, http.MethodPatch, "/api/v1/workspace/slug", map[string]any{"slug": "renamed-desk"}, send)
	if got := getRequest(t, desk.api, agent, created.RequestID); got.ApprovableBy != "admin" {
		t.Errorf("approvable_by = %q, want admin", got.ApprovableBy)
	}
	response := approve(t, desk.api, agent, created.RequestID, nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("approve by an agent status=%d body=%s, want 403", response.Code, response.Body.String())
	}
	if got := getRequest(t, desk.api, desk.admin, created.RequestID); got.Status != "pending" || got.DecidedBy != nil {
		t.Fatalf("a refused approval changed the request: %+v", got)
	}
	if response := approve(t, desk.api, desk.admin, created.RequestID, nil); response.Code != http.StatusOK {
		t.Fatalf("an admin approves: status=%d body=%s", response.Code, response.Body.String())
	}
	var me struct {
		Workspace struct {
			Slug string `json:"slug"`
		} `json:"workspace"`
	}
	decode(t, request(t, desk.api, http.MethodGet, "/api/v1/auth/me", nil, desk.admin, nil), &me)
	if me.Workspace.Slug != "renamed-desk" {
		t.Fatalf("slug = %q after the admin approved, want renamed-desk", me.Workspace.Slug)
	}
}

func TestApprovalRequiredRoutesBecomeRequestsEvenForTheSendTier(t *testing.T) {
	desk := newRequestDesk(t, "Approval Desk")
	send := issueToken(t, desk.api, desk.admin, "sender", "send").Token
	cases := []struct {
		method, path string
		body         any
	}{
		{http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "minted", "scope": "send"}},
		{http.MethodGet, "/api/v1/export", nil},
		{http.MethodPost, "/api/v1/export/link", nil},
		{http.MethodDelete, "/api/v1/channels/email", nil},
		{http.MethodPost, "/api/v1/channels/email/secret", nil},
		{http.MethodPatch, "/api/v1/workspace/slug", map[string]any{"slug": "approval-two"}},
	}
	for _, item := range cases {
		created := accepted(t, desk.api, item.method, item.path, item.body, send)
		if created.Kind != "action" {
			t.Errorf("%s %s kind=%q", item.method, item.path, created.Kind)
		}
	}
	if pending := listRequests(t, desk.api, desk.admin, "?status=pending"); len(pending) != len(cases) {
		t.Fatalf("pending = %d, want %d", len(pending), len(cases))
	}
}

func TestApprovedCredentialIsShownToTheApproverOnlyAndNeverStored(t *testing.T) {
	desk := newRequestDesk(t, "Credential Desk")
	send := issueToken(t, desk.api, desk.admin, "sender", "send").Token
	created := accepted(t, desk.api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "minted", "scope": "read"}, send)
	response := approve(t, desk.api, desk.admin, created.RequestID, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", response.Code, response.Body.String())
	}
	var outcome approvedView
	decode(t, response, &outcome)
	if !strings.Contains(string(outcome.Result), "cdk_agent_") {
		t.Fatalf("the approver must see the new token once: %s", outcome.Result)
	}
	if outcome.Request.Status != "approved" || outcome.Request.ResultStatus == nil || *outcome.Request.ResultStatus != http.StatusCreated {
		t.Errorf("request = %+v", outcome.Request)
	}
	for _, token := range []string{send, desk.admin} {
		stored := getRequest(t, desk.api, token, created.RequestID)
		if strings.Contains(string(stored.Result), "cdk_agent_") || stored.ResultNote == nil {
			t.Errorf("stored request leaks the credential or does not say why it is missing: %+v", stored)
		}
	}
}

func TestMultipartCallAboveTheTierIsRefusedWithAWayForward(t *testing.T) {
	desk := newRequestDesk(t, "Files Desk")
	for tier, kind := range map[string]string{"read": "note", "draft": "outgoing"} {
		token := issueToken(t, desk.api, desk.admin, tier+" files", tier).Token
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		_ = writer.WriteField("kind", kind)
		_ = writer.WriteField("body", "With a file")
		part, _ := writer.CreateFormFile("files", "log.txt")
		_, _ = part.Write([]byte("log"))
		_ = writer.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/conversations/"+desk.conversation+"/messages", &buffer)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+token)
		response := serve(desk.api, req)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "attach the files after") {
			t.Errorf("%s token %s with a file: status=%d body=%s, want 403 telling it to attach files after approval", tier, kind, response.Code, response.Body.String())
		}
	}
	if pending := listRequests(t, desk.api, desk.admin, ""); len(pending) != 0 {
		t.Errorf("a refused multipart call stored a request: %+v", pending)
	}
}

func TestTokenSeesAndCancelsOnlyItsOwnRequests(t *testing.T) {
	desk := newRequestDesk(t, "Own Desk")
	first := issueToken(t, desk.api, desk.admin, "first", "read").Token
	second := issueToken(t, desk.api, desk.admin, "second", "read").Token
	created := accepted(t, desk.api, http.MethodPatch, "/api/v1/conversations/"+desk.conversation, map[string]any{"status": "closed"}, first)

	if items := listRequests(t, desk.api, second, ""); len(items) != 0 {
		t.Errorf("another token lists %+v", items)
	}
	for _, probe := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/agent-requests/" + created.RequestID},
		{http.MethodPost, "/api/v1/agent-requests/" + created.RequestID + "/cancel"},
	} {
		if response := request(t, desk.api, probe.method, probe.path, nil, second, nil); response.Code != http.StatusNotFound {
			t.Errorf("another token %s %s: status=%d, want 404", probe.method, probe.path, response.Code)
		}
	}
	// Approving and rejecting are for people.
	for _, action := range []string{"approve", "reject"} {
		response := request(t, desk.api, http.MethodPost, "/api/v1/agent-requests/"+created.RequestID+"/"+action, nil, first, nil)
		if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "person") {
			t.Errorf("token %s its own request: status=%d body=%s, want 403 for people only", action, response.Code, response.Body.String())
		}
	}
	// A teammate who does not own the token rejects; only the owner (or the token) cancels.
	agent := joinAsAgent(t, desk.api, desk.admin, "agent@own-desk.example.com")
	if response := request(t, desk.api, http.MethodPost, "/api/v1/agent-requests/"+created.RequestID+"/cancel", nil, agent, nil); response.Code != http.StatusForbidden {
		t.Errorf("teammate cancels: status=%d, want 403", response.Code)
	}
	if response := request(t, desk.api, http.MethodPost, "/api/v1/agent-requests/"+created.RequestID+"/cancel", nil, desk.admin, nil); response.Code != http.StatusOK {
		t.Errorf("owner cancels: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRequestOfARevokedTokenCannotBeApproved(t *testing.T) {
	desk := newRequestDesk(t, "Revoked Desk")
	issued := issueToken(t, desk.api, desk.admin, "leaked", "read")
	created := accepted(t, desk.api, http.MethodPatch, "/api/v1/conversations/"+desk.conversation, map[string]any{"status": "closed"}, issued.Token)
	if response := request(t, desk.api, http.MethodDelete, "/api/v1/auth/sessions/"+issued.Session.ID, nil, desk.admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("revoke status=%d", response.Code)
	}
	if got := getRequest(t, desk.api, desk.admin, created.RequestID); !got.RequestedBy.TokenRevoked {
		t.Errorf("request of a revoked token must say so: %+v", got.RequestedBy)
	}
	if response := approve(t, desk.api, desk.admin, created.RequestID, nil); response.Code != http.StatusConflict {
		t.Errorf("approve after revoke: status=%d body=%s, want 409", response.Code, response.Body.String())
	}
}

func TestAgentRequestsStayInTheirWorkspace(t *testing.T) {
	alpha := newRequestDesk(t, "Alpha Requests")
	api := alpha.api
	betaAdmin := createSession(t, api, alpha.service, "Beta Requests", "admin@beta-requests.example.com")
	betaRead := issueToken(t, api, betaAdmin, "beta reader", "read").Token
	read := issueToken(t, api, alpha.admin, "alpha reader", "read").Token
	created := accepted(t, api, http.MethodPatch, "/api/v1/conversations/"+alpha.conversation, map[string]any{"status": "closed"}, read)

	if items := listRequests(t, api, betaAdmin, ""); len(items) != 0 {
		t.Errorf("another workspace lists %+v", items)
	}
	if items := listRequests(t, api, betaRead, ""); len(items) != 0 {
		t.Errorf("another workspace's token lists %+v", items)
	}
	for _, probe := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/agent-requests/" + created.RequestID},
		{http.MethodPost, "/api/v1/agent-requests/" + created.RequestID + "/approve"},
		{http.MethodPost, "/api/v1/agent-requests/" + created.RequestID + "/reject"},
		{http.MethodPost, "/api/v1/agent-requests/" + created.RequestID + "/cancel"},
	} {
		if response := request(t, api, probe.method, probe.path, nil, betaAdmin, nil); response.Code != http.StatusNotFound {
			t.Errorf("another workspace %s %s: status=%d body=%s, want 404", probe.method, probe.path, response.Code, response.Body.String())
		}
	}
	// A token cannot file a request about another workspace's conversation.
	if response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+alpha.conversation, map[string]any{"status": "closed"}, betaRead, nil); response.Code != http.StatusNotFound {
		t.Errorf("request on another workspace's conversation: status=%d body=%s, want 404", response.Code, response.Body.String())
	}
	if got := getRequest(t, api, alpha.admin, created.RequestID); got.Status != "pending" {
		t.Errorf("alpha's request after beta's probes = %q, want pending", got.Status)
	}
	if got := conversationStatus(t, api, alpha.admin, alpha.conversation); got != "open" {
		t.Errorf("alpha's conversation = %q, want open", got)
	}
}

// A token below a route's tier, and every token on an approval route, gets 202: the contract says so
// on each of those operations.
func TestEveryRequestableRouteDeclaresTheAgentRequestAnswer(t *testing.T) {
	document := loadContract(t)
	for key, access := range httpapi.TokenAccessTable() {
		requestable := access.Approval || access.Tier == "draft" || access.Tier == "send" || access.Tier == "handler"
		if !requestable {
			continue
		}
		method, pattern, _ := strings.Cut(key, " ")
		operation := document.Paths.Find(pattern).GetOperation(method)
		// A body that is only a file (an image upload) is never filed: it gets 403 files_need_approval,
		// which TestEveryRouteIsOpenToTokensOnlyFromItsTier checks.
		if operation != nil && operation.RequestBody != nil && operation.RequestBody.Value.Content.Get("application/json") == nil {
			continue
		}
		if kbFileRouteKeys[key] { // a video upload's routes are refused the same way
			continue
		}
		if operation == nil || operation.Responses.Value("202") == nil {
			t.Errorf("%s can become an agent request but contracts/openapi.yaml declares no 202 for it: add '202': {$ref: '#/components/responses/AgentRequestAccepted'}", key)
		}
	}
}
