package httpapi_test

import (
	"net/http"
	"testing"
)

type countedPage struct {
	Items        []struct{ ID string } `json:"items"`
	StatusCounts map[string]int        `json:"status_counts"`
}

func countsOf(t *testing.T, api http.Handler, token, query string) countedPage {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/conversations?"+query, nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list %q: %d %s", query, response.Code, response.Body.String())
	}
	var page countedPage
	decode(t, response, &page)
	return page
}

// The status tabs say how many conversations each holds under the filters on screen (Open 23, Pending 4):
// counts=true adds them to the list, every status counted with the other filters, and only this workspace's.
func TestConversationListCountsEachStatusUnderTheOtherFilters(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Count Desk", "admin@count.example.com")
	other := createSession(t, api, service, "Other Desk", "admin@count-other.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@count.example"})
	otherContact := create(t, api, other, "/api/v1/contacts", map[string]any{"name": "Bo", "email": "bo@count.example"})
	open := func(token, contact, priority, status string) string {
		id := create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Counted"})
		changes := map[string]any{"priority": priority}
		if status != "open" {
			changes["status"] = status
		}
		if response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+id, changes, token, nil); response.Code != http.StatusOK {
			t.Fatalf("patch: %d %s", response.Code, response.Body.String())
		}
		return id
	}
	open(admin, contact, "urgent", "open")
	open(admin, contact, "urgent", "open")
	open(admin, contact, "normal", "open")
	open(admin, contact, "urgent", "pending")
	open(admin, contact, "urgent", "resolved")
	spam := open(admin, contact, "urgent", "open")
	markSpam(t, api, admin, true, spam)
	open(other, otherContact, "urgent", "open")

	all := countsOf(t, api, admin, "counts=true")
	want := map[string]int{"open": 3, "pending": 1, "resolved": 1, "closed": 0, "spam": 1}
	for status, n := range want {
		if all.StatusCounts[status] != n {
			t.Fatalf("counts = %v, want %v", all.StatusCounts, want)
		}
	}
	urgent := countsOf(t, api, admin, "counts=true&status=pending&priority=urgent")
	if urgent.StatusCounts["open"] != 2 || urgent.StatusCounts["pending"] != 1 || len(urgent.Items) != 1 {
		t.Fatalf("urgent counts = %v with %d rows, want open 2, pending 1 and the one pending row", urgent.StatusCounts, len(urgent.Items))
	}
	if plain := countsOf(t, api, admin, ""); plain.StatusCounts != nil {
		t.Fatalf("counts without counts=true: %v", plain.StatusCounts)
	}
}
