package httpapi_test

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

// Due first is the inbox's order; without reply targets nothing is due, so it orders by who has waited
// longest for the team (the oldest customer message first), never by the last change, as Today does.
func TestDueFirstWithoutTargetsOrdersByWaitingLongest(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Due Desk", "admin@due.example.com")
	secret := connectEmail(t, api, admin, "support@due.example.com")
	mailFrom := func(id, subject string) string {
		return *inboundOK(t, api, secret, mail([]string{"From: Customer " + id + " <" + id + "@customer.example>", "To: support@due.example.com",
			"Subject: " + subject, "Message-ID: <" + id + "@customer.example>"}, subject+" body.")).ConversationID
	}
	first := mailFrom("first", "Waiting since first")
	second := mailFrom("second", "Waiting since second")
	// A private note on second changes it last, not who waits: first still waits longest.
	if response := request(t, api, http.MethodPost, "/api/v1/conversations/"+second+"/messages", map[string]any{"kind": "note", "body": "Looking"}, admin, nil); response.Code != http.StatusCreated {
		t.Fatalf("note: %d %s", response.Code, response.Body.String())
	}
	response := request(t, api, http.MethodGet, "/api/v1/conversations?sort=due", nil, admin, nil)
	var page struct {
		Items []struct{ ID string } `json:"items"`
	}
	decode(t, response, &page)
	var got []string
	for _, item := range page.Items {
		got = append(got, item.ID)
	}
	if strings.Join(got, ",") != first+","+second {
		t.Fatalf("sort=due without targets = %v, want the longest waiting first: %s, %s", got, first, second)
	}
}

// Only an open conversation whose customer wrote last waits on the team. A Zalo team often resolves a
// conversation without replying in the chat, so its last public message stays the customer's: it must
// not rank as waiting above the open ones. Pending waits on the customer. They follow by last update.
func TestWaitingLongestCountsOnlyOpenConversationsAsWaiting(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Zalo Desk", "admin@zalo-wait.example.com")
	workspace := workspaceID(t, api, admin)
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Hana", "email": "hana@zalo-wait.example"})
	ctx := context.Background()
	day := func(d, hour int) time.Time { return time.Date(2026, 11, d, hour, 0, 0, 0, time.UTC) }
	names := map[string]string{}
	for _, row := range []struct {
		name, status string
		customerAt   time.Time
		updatedAt    time.Time
	}{
		{"resolved-nov3", "resolved", day(3, 9), day(3, 10)},
		{"resolved-nov4", "resolved", day(4, 9), day(4, 10)},
		{"resolved-nov5", "resolved", day(5, 9), day(5, 10)},
		{"closed-nov1", "closed", day(1, 9), day(6, 10)},
		{"open-nov8", "open", day(8, 9), day(8, 10)},
		{"pending-nov2", "pending", day(2, 9), day(9, 10)},
		// Updated last of all: open conversations are ordered by the customer's message, not by the last update.
		{"open-nov12", "open", day(12, 9), day(14, 10)},
	} {
		id := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": row.name})
		names[id] = row.name
		if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,created_at) VALUES($1,$2,'incoming',$3,$4)`,
			workspace, id, "Message of "+row.name, row.customerAt); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE conversations SET status=$2,created_at=$3,updated_at=$4 WHERE id=$1`, id, row.status, row.customerAt, row.updatedAt); err != nil {
			t.Fatal(err)
		}
	}
	// Open first, the oldest customer message first; then the rest by last update, newest first.
	want := []string{"open-nov8", "open-nov12", "pending-nov2", "closed-nov1", "resolved-nov5", "resolved-nov4", "resolved-nov3"}
	for _, query := range []string{"sort=waiting_longest", "sort=due"} {
		response := request(t, api, http.MethodGet, "/api/v1/conversations?"+query, nil, admin, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET ?%s = %d %s", query, response.Code, response.Body.String())
		}
		var page struct {
			Items []struct{ ID string } `json:"items"`
			Sort  string                `json:"sort"`
		}
		decode(t, response, &page)
		got := make([]string, 0, len(page.Items))
		for _, item := range page.Items {
			got = append(got, names[item.ID])
		}
		if !slices.Equal(got, want) {
			t.Fatalf("?%s = %v, want %v", query, got, want)
		}
		// The answer names the order it used, so the inbox and an agent can say it: due falls back while
		// reply targets are off.
		if page.Sort != "waiting_longest" {
			t.Fatalf("?%s answered sort %q, want waiting_longest", query, page.Sort)
		}
	}
}
