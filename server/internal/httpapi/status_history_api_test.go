package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

type statusChange struct {
	from, to, actor, source string
}

// Every way a status changes leaves one row of history with who changed it: a person, a bulk update,
// a customer reply that reopens (no one), and a change to the same status leaves nothing.
func TestEveryWayAStatusChangesIsRecordedWithWhoChangedIt(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "History Desk", "admin@history.example.com")
	adminID := queryString(t, pool, `SELECT id::text FROM agents WHERE email='admin@history.example.com'`)
	secret := connectEmail(t, api, admin, "help@history.example.com")
	first := inboundOK(t, api, secret, mail([]string{
		"From: erin@customer.example", "To: help@history.example.com",
		"Subject: Invoice", "Message-ID: <invoice-1@customer.example>",
	}, "Where is my invoice?"))
	conversationID := *first.ConversationID
	_ = create(t, api, admin, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Attached."})
	ourMessageID := queryString(t, pool, `SELECT substr(external_id,7) FROM messages WHERE conversation_id=$1 AND kind='outgoing'`, conversationID)

	for _, status := range []string{"resolved", "resolved"} {
		if response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+conversationID, map[string]any{"status": status}, admin, nil); response.Code != http.StatusOK {
			t.Fatalf("resolve status=%d body=%s", response.Code, response.Body.String())
		}
	}
	_ = inboundOK(t, api, secret, mail([]string{
		"From: erin@customer.example", "To: help@history.example.com",
		"Subject: Re: Invoice", "Message-ID: <invoice-2@customer.example>",
		"In-Reply-To: <" + ourMessageID + ">",
	}, "Thanks!"))
	if response := request(t, api, http.MethodPatch, "/api/v1/conversations", map[string]any{"ids": []string{conversationID}, "changes": map[string]any{"status": "closed"}}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("bulk close status=%d body=%s", response.Code, response.Body.String())
	}

	rows, err := pool.Query(context.Background(), `SELECT from_status,to_status,COALESCE(actor_agent_id::text,''),source
		FROM conversation_status_changes WHERE conversation_id=$1 ORDER BY changed_at,id`, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	var got []statusChange
	for rows.Next() {
		var change statusChange
		if err := rows.Scan(&change.from, &change.to, &change.actor, &change.source); err != nil {
			t.Fatal(err)
		}
		got = append(got, change)
	}
	rows.Close()
	want := []statusChange{
		{"open", "resolved", adminID, "colliedesk"},
		{"resolved", "open", "", "colliedesk"},
		{"open", "closed", adminID, "colliedesk"},
	}
	if len(got) != len(want) {
		t.Fatalf("history = %+v, want %+v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("history = %+v, want %+v", got, want)
		}
	}

	// The history leaves with the export, inside its conversation.
	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d", response.Code)
	}
	entries := unzip(t, response.Body.Bytes())
	conversations := ndjsonLines(t, entries["conversations.ndjson"])
	if len(conversations) != 1 {
		t.Fatalf("conversations.ndjson has %d lines", len(conversations))
	}
	changes, _ := conversations[0]["status_changes"].([]any)
	if len(changes) != 3 {
		t.Fatalf("exported status_changes = %v", conversations[0]["status_changes"])
	}
	firstChange := changes[0].(map[string]any)
	if firstChange["from_status"] != "open" || firstChange["to_status"] != "resolved" || firstChange["actor_agent_id"] != adminID ||
		firstChange["source"] != "colliedesk" || !strings.HasSuffix(firstChange["changed_at"].(string), "Z") {
		t.Fatalf("exported change = %v", firstChange)
	}
}

// Changes are in the order they happened, also several in one transaction: the time is when the
// status changed, not when the transaction began.
func TestStatusChangesKeepTheOrderTheyHappenedIn(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Order Desk", "admin@order.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Fay", "email": "fay@customer.example"})
	conversationID := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Order"})
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"resolved", "open", "closed", "pending"} {
		if _, err := tx.Exec(ctx, `UPDATE conversations SET status=$2 WHERE id=$1`, conversationID, status); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT to_status FROM conversation_status_changes WHERE conversation_id=$1 ORDER BY changed_at,id`, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var status string
		if err := rows.Scan(&status); err != nil {
			t.Fatal(err)
		}
		got = append(got, status)
	}
	rows.Close()
	if strings.Join(got, ",") != "resolved,open,closed,pending" {
		t.Fatalf("history order = %v", got)
	}
}
