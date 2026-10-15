package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"
	"time"
)

type activityBody struct {
	Now              time.Time `json:"now"`
	NewConversations []struct {
		ID           string    `json:"id"`
		Subject      string    `json:"subject"`
		ContactName  string    `json:"contact_name"`
		ContactEmail string    `json:"contact_email"`
		CreatedAt    time.Time `json:"created_at"`
	} `json:"new_conversations"`
	AssignedToMe []struct {
		ID         string    `json:"id"`
		Subject    string    `json:"subject"`
		AssignedAt time.Time `json:"assigned_at"`
	} `json:"assigned_to_me"`
}

func inboxActivity(t *testing.T, api http.Handler, token string, since time.Time) activityBody {
	t.Helper()
	path := "/api/v1/inbox/activity"
	if !since.IsZero() {
		path += "?since=" + url.QueryEscape(since.Format(time.RFC3339Nano))
	}
	response := request(t, api, http.MethodGet, path, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, response.Code, response.Body.String())
	}
	var body activityBody
	decode(t, response, &body)
	return body
}

func agentID(t *testing.T, api http.Handler, token string) string {
	t.Helper()
	var me struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/auth/me", nil, token, nil), &me)
	return me.Agent.ID
}

func TestInboxActivityListsNewConversationOnce(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Activity New", "admin@activity-new.example.com")
	contact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Ada Lovelace", "email": "ada@activity-new.example.com"})

	first := inboxActivity(t, api, token, time.Time{})
	if first.Now.IsZero() || len(first.NewConversations) != 0 || len(first.AssignedToMe) != 0 {
		t.Fatalf("initial cursor = %+v, want now and empty lists", first)
	}
	id := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer on fire"})

	second := inboxActivity(t, api, token, first.Now)
	if len(second.NewConversations) != 1 || second.NewConversations[0].ID != id {
		t.Fatalf("new conversations = %+v, want only %s", second.NewConversations, id)
	}
	if got := second.NewConversations[0]; got.Subject != "Printer on fire" || got.ContactName != "Ada Lovelace" || got.ContactEmail != "ada@activity-new.example.com" {
		t.Fatalf("activity item = %+v", got)
	}
	third := inboxActivity(t, api, token, second.Now)
	if len(third.NewConversations) != 0 {
		t.Fatalf("conversation listed again on the next cursor: %+v", third.NewConversations)
	}
}

func TestInboxActivityAssignmentNotifiesOnlyTheAssignee(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Activity Assign", "admin@activity-assign.example.com")
	_, inviteURL := createContractInvite(t, api, admin, "x@activity-assign.example.com", "agent")
	agentX := redeemLabelled(t, api, inviteURL, "Agent X")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Grace", "email": "grace@activity-assign.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Refund"})
	cursor := inboxActivity(t, api, admin, time.Time{}).Now
	patch := func(token string, assignee string) {
		t.Helper()
		response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"assignee_id": assignee}, token, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("assign status=%d body=%s", response.Code, response.Body.String())
		}
	}

	patch(admin, agentID(t, api, agentX))
	forX := inboxActivity(t, api, agentX, cursor)
	if len(forX.AssignedToMe) != 1 || forX.AssignedToMe[0].ID != conversation || forX.AssignedToMe[0].AssignedAt.IsZero() {
		t.Fatalf("assignee activity = %+v", forX.AssignedToMe)
	}
	if forAdmin := inboxActivity(t, api, admin, cursor); len(forAdmin.AssignedToMe) != 0 {
		t.Fatalf("assigner was notified: %+v", forAdmin.AssignedToMe)
	}
	if again := inboxActivity(t, api, agentX, forX.Now); len(again.AssignedToMe) != 0 {
		t.Fatalf("assignment listed again on the next cursor: %+v", again.AssignedToMe)
	}
}

func TestInboxActivitySelfAssignmentIsNotListed(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Activity Self", "admin@activity-self.example.com")
	_, inviteURL := createContractInvite(t, api, admin, "x@activity-self.example.com", "agent")
	agentX := redeemLabelled(t, api, inviteURL, "Agent X")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Linus", "email": "linus@activity-self.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Login loop"})
	cursor := inboxActivity(t, api, agentX, time.Time{}).Now

	response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"assignee_id": agentID(t, api, agentX)}, agentX, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("self-assign status=%d body=%s", response.Code, response.Body.String())
	}
	if got := inboxActivity(t, api, agentX, cursor); len(got.AssignedToMe) != 0 {
		t.Fatalf("self-assignment listed: %+v", got.AssignedToMe)
	}
}

func TestInboxActivityStaysInsideTheWorkspace(t *testing.T) {
	api, service := testAPI(t)
	tokenA := createSession(t, api, service, "Activity Alpha", "admin@activity-alpha.example.com")
	tokenB := createSession(t, api, service, "Activity Beta", "admin@activity-beta.example.com")
	contactB := create(t, api, tokenB, "/api/v1/contacts", map[string]any{"name": "Beta", "email": "beta@activity-beta.example.com"})
	cursor := inboxActivity(t, api, tokenA, time.Time{}).Now
	_ = create(t, api, tokenB, "/api/v1/conversations", map[string]any{"contact_id": contactB, "subject": "Beta only"})

	if got := inboxActivity(t, api, tokenA, cursor); len(got.NewConversations) != 0 || len(got.AssignedToMe) != 0 {
		t.Fatalf("workspace A saw workspace B activity: %+v", got)
	}
	if got := inboxActivity(t, api, tokenB, cursor); len(got.NewConversations) != 1 {
		t.Fatalf("workspace B own activity = %+v, want 1 new conversation", got.NewConversations)
	}
}

func TestInboxActivityRejectsBadCursorAndAnonymous(t *testing.T) {
	api, service := testAPI(t)
	token := createSession(t, api, service, "Activity Bad", "admin@activity-bad.example.com")
	if response := request(t, api, http.MethodGet, "/api/v1/inbox/activity?since=yesterday", nil, token, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("bad since status=%d, want 400", response.Code)
	}
	if response := request(t, api, http.MethodGet, "/api/v1/inbox/activity", nil, "", nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d, want 401", response.Code)
	}
}
