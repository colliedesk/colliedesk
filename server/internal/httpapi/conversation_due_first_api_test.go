package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
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
