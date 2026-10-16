package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

type activityBody struct {
	Now              time.Time `json:"now"`
	NewConversations []struct {
		ID          string    `json:"id"`
		Subject     string    `json:"subject"`
		ContactName string    `json:"contact_name"`
		CreatedAt   time.Time `json:"created_at"`
	} `json:"new_conversations"`
	AssignedToMe []struct {
		ID         string    `json:"id"`
		Subject    string    `json:"subject"`
		AssignedAt time.Time `json:"assigned_at"`
	} `json:"assigned_to_me"`
	Updated []updatedActivity `json:"updated"`
}

type updatedActivity struct {
	ID          string     `json:"id"`
	Subject     string     `json:"subject"`
	ContactName string     `json:"contact_name"`
	AssigneeID  *string    `json:"assignee_id"`
	UpdatedAt   time.Time  `json:"updated_at"`
	IncomingAt  *time.Time `json:"incoming_at"`
}

func updatedByID(items []updatedActivity, id string) (updatedActivity, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return updatedActivity{}, false
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
	if got := second.NewConversations[0]; got.Subject != "Printer on fire" || got.ContactName != "Ada Lovelace" {
		t.Fatalf("activity item = %+v", got)
	}
}

// A conversation can become visible after a poll already moved its cursor past created_at (its
// transaction was still open), so the server answers with a lookback and the browser removes
// duplicates. The lookback is bounded: older rows never come back.
func TestInboxActivityLookbackCatchesLateCommitsAndStaysBounded(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	token := createSession(t, api, service, "Activity Late", "admin@activity-late.example.com")
	contact := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Late", "email": "late@activity-late.example.com"})
	ctx := context.Background()
	workspace := workspaceID(t, api, token)
	cursor := inboxActivity(t, api, token, time.Time{}).Now

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var late string
	if err := tx.QueryRow(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,created_at)
		VALUES($1,$2,'Late mail',now()-interval '1 second') RETURNING id::text`, workspace, contact).Scan(&late); err != nil {
		t.Fatal(err)
	}
	during := inboxActivity(t, api, token, cursor)
	if len(during.NewConversations) != 0 {
		t.Fatalf("uncommitted conversation listed: %+v", during.NewConversations)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	after := inboxActivity(t, api, token, during.Now)
	if len(after.NewConversations) != 1 || after.NewConversations[0].ID != late {
		t.Fatalf("late commit missed by the next poll: %+v", after.NewConversations)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversations(workspace_id,contact_id,subject,created_at)
		VALUES($1,$2,'Old mail',now()-interval '10 minutes')`, workspace, contact); err != nil {
		t.Fatal(err)
	}
	for _, item := range inboxActivity(t, api, token, time.Now().UTC()).NewConversations {
		if item.Subject == "Old mail" {
			t.Fatalf("a conversation older than the lookback was listed: %+v", item)
		}
	}
}

func TestInboxActivityUnassignAndReassign(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Activity Reassign", "admin@activity-reassign.example.com")
	_, urlX := createContractInvite(t, api, admin, "x@activity-reassign.example.com", "agent")
	_, urlY := createContractInvite(t, api, admin, "y@activity-reassign.example.com", "agent")
	agentX, agentY := redeemLabelled(t, api, urlX, "Agent X"), redeemLabelled(t, api, urlY, "Agent Y")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Re", "email": "re@activity-reassign.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Moved"})
	patch := func(assignee any) {
		t.Helper()
		if response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"assignee_id": assignee}, admin, nil); response.Code != http.StatusOK {
			t.Fatalf("assign status=%d body=%s", response.Code, response.Body.String())
		}
	}
	x, y := agentID(t, api, agentX), agentID(t, api, agentY)

	patch(x)
	patch(nil)
	cursor := time.Now().UTC().Add(time.Second)
	if got := inboxActivity(t, api, agentX, cursor.Add(-time.Minute)); len(got.AssignedToMe) != 0 {
		t.Fatalf("unassigned conversation still listed for X: %+v", got.AssignedToMe)
	}
	patch(x)
	patch(y)
	if got := inboxActivity(t, api, agentX, cursor.Add(-time.Minute)); len(got.AssignedToMe) != 0 {
		t.Fatalf("X was told about a conversation now assigned to Y: %+v", got.AssignedToMe)
	}
	if got := inboxActivity(t, api, agentY, cursor.Add(-time.Minute)); len(got.AssignedToMe) != 1 || got.AssignedToMe[0].ID != conversation {
		t.Fatalf("Y activity = %+v, want the reassigned conversation", got.AssignedToMe)
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

func TestInboxActivityReportsACustomerReplyOnAnExistingConversation(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Activity Reply", "admin@activity-reply.example.com")
	secret := connectEmail(t, api, admin, "support@activity-reply.example.com")
	first := inboundOK(t, api, secret, mail([]string{"From: Ada <ada@customer.example>", "To: support@activity-reply.example.com", "Subject: Printer", "Message-ID: <first@customer.example>"}, "It is on fire."))
	cursor := inboxActivity(t, api, admin, time.Time{}).Now

	reply := inboundOK(t, api, secret, mail([]string{"From: Ada <ada@customer.example>", "To: support@activity-reply.example.com", "Subject: Re: Printer",
		"Message-ID: <second@customer.example>", "In-Reply-To: <first@customer.example>", "References: <first@customer.example>"}, "Still on fire."))
	if reply.ConversationID == nil || *reply.ConversationID != *first.ConversationID {
		t.Fatalf("reply was not threaded: %+v", reply)
	}

	got := inboxActivity(t, api, admin, cursor)
	item, found := updatedByID(got.Updated, *first.ConversationID)
	if len(got.Updated) != 1 || !found || item.IncomingAt == nil || item.Subject != "Printer" || item.ContactName != "Ada" || item.UpdatedAt.IsZero() {
		t.Fatalf("updated = %+v, want the replied conversation with an incoming timestamp", got.Updated)
	}
}

func TestInboxActivityReportsAnotherAgentsChangeWithoutAnIncomingMark(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Activity Change", "admin@activity-change.example.com")
	_, inviteURL := createContractInvite(t, api, admin, "b@activity-change.example.com", "agent")
	agentB := redeemLabelled(t, api, inviteURL, "Agent B")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Grace", "email": "grace@activity-change.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Refund"})
	cursor := inboxActivity(t, api, admin, time.Time{}).Now

	if response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversation, map[string]any{"status": "resolved"}, agentB, nil); response.Code != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "note", "body": "Refunded."}, agentB, nil); response.Code != http.StatusCreated {
		t.Fatalf("note status=%d body=%s", response.Code, response.Body.String())
	}

	got := inboxActivity(t, api, admin, cursor)
	item, found := updatedByID(got.Updated, conversation)
	if !found || item.IncomingAt != nil {
		t.Fatalf("updated = %+v, want the changed conversation with no incoming timestamp (nothing to notify)", got.Updated)
	}
}

func TestInboxActivityUpdatedStaysInsideTheWorkspaceAndTheLookback(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	tokenA := createSession(t, api, service, "Activity Upd Alpha", "admin@activity-upd-alpha.example.com")
	tokenB := createSession(t, api, service, "Activity Upd Beta", "admin@activity-upd-beta.example.com")
	contactA := create(t, api, tokenA, "/api/v1/contacts", map[string]any{"name": "Alpha", "email": "alpha@activity-upd-alpha.example.com"})
	contactB := create(t, api, tokenB, "/api/v1/contacts", map[string]any{"name": "Beta", "email": "beta@activity-upd-beta.example.com"})
	conversationA := create(t, api, tokenA, "/api/v1/conversations", map[string]any{"contact_id": contactA, "subject": "Alpha only"})
	conversationB := create(t, api, tokenB, "/api/v1/conversations", map[string]any{"contact_id": contactB, "subject": "Beta only"})
	cursor := inboxActivity(t, api, tokenA, time.Time{}).Now
	if _, err := pool.Exec(context.Background(), `INSERT INTO messages(workspace_id,conversation_id,kind,body)
		SELECT workspace_id,id,'incoming','Hello' FROM conversations WHERE id=$1::uuid`, conversationB); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE conversations SET updated_at=now() WHERE id=$1::uuid`, conversationB); err != nil {
		t.Fatal(err)
	}

	if got := inboxActivity(t, api, tokenA, cursor); func() bool { _, found := updatedByID(got.Updated, conversationB); return found }() {
		t.Fatalf("workspace A saw workspace B update: %+v", got.Updated)
	}
	if got := inboxActivity(t, api, tokenB, cursor); func() bool {
		item, found := updatedByID(got.Updated, conversationB)
		return !found || item.IncomingAt == nil
	}() {
		t.Fatalf("workspace B own update missing: %+v", got.Updated)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE conversations SET updated_at=now()-interval '10 minutes' WHERE id=$1::uuid`, conversationA); err != nil {
		t.Fatal(err)
	}
	if got := inboxActivity(t, api, tokenA, time.Now().UTC()); func() bool { _, found := updatedByID(got.Updated, conversationA); return found }() {
		t.Fatalf("an update older than the lookback was listed: %+v", got.Updated)
	}
}

func TestInboxActivityKeepsACustomerReplyWhenABulkChangeFillsTheList(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Activity Bulk", "admin@activity-bulk.example.com")
	secret := connectEmail(t, api, admin, "support@activity-bulk.example.com")
	first := inboundOK(t, api, secret, mail([]string{"From: Ada <ada@customer.example>", "To: support@activity-bulk.example.com", "Subject: Printer", "Message-ID: <bulk-first@customer.example>"}, "It is on fire."))
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Grace", "email": "grace@activity-bulk.example.com"})
	others := make([]string, 0, store.MaxBulkConversations)
	for index := 0; index < store.MaxBulkConversations; index++ {
		others = append(others, create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": fmt.Sprintf("Old %d", index)}))
	}
	cursor := inboxActivity(t, api, admin, time.Time{}).Now

	inboundOK(t, api, secret, mail([]string{"From: Ada <ada@customer.example>", "To: support@activity-bulk.example.com", "Subject: Re: Printer",
		"Message-ID: <bulk-second@customer.example>", "In-Reply-To: <bulk-first@customer.example>", "References: <bulk-first@customer.example>"}, "Still on fire."))
	if response := request(t, api, http.MethodPatch, "/api/v1/conversations", map[string]any{"ids": others, "changes": map[string]any{"status": "resolved"}}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("bulk status=%d body=%s", response.Code, response.Body.String())
	}

	got := inboxActivity(t, api, admin, cursor)
	item, found := updatedByID(got.Updated, *first.ConversationID)
	if !found || item.IncomingAt == nil {
		t.Fatalf("the replied conversation is missing from updated (%d items) or has no incoming time: %+v", len(got.Updated), item)
	}
	if len(got.Updated) > store.InboxActivityLimit {
		t.Fatalf("updated has %d items, cap is %d", len(got.Updated), store.InboxActivityLimit)
	}
}

func TestInboxActivityIgnoresACursorOlderThanTenMinutes(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Activity Old Cursor", "admin@activity-old-cursor.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Grace", "email": "grace@activity-old-cursor.example.com"})
	old := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Last month"})
	recent := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Just now"})
	if _, err := pool.Exec(context.Background(), `UPDATE conversations SET created_at=now()-interval '20 minutes', updated_at=now()-interval '20 minutes' WHERE id=$1::uuid`, old); err != nil {
		t.Fatal(err)
	}

	got := inboxActivity(t, api, admin, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC))
	created := map[string]bool{}
	for _, item := range got.NewConversations {
		created[item.ID] = true
	}
	_, oldUpdated := updatedByID(got.Updated, old)
	if created[old] || oldUpdated {
		t.Fatalf("a cursor from 1970 listed a conversation from 20 minutes ago: new=%+v updated=%+v", got.NewConversations, got.Updated)
	}
	if !created[recent] {
		t.Fatalf("the recent conversation is missing: %+v", got.NewConversations)
	}
}
