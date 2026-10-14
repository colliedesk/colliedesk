package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
)

func TestConversationPaginationUsesStableActivityOrder(t *testing.T) {
	api, authService, pool := testAPIWithPool(t)
	token := createSession(t, api, authService, "Page Desk", "admin@page.example.com")
	contactID := create(t, api, token, "/api/v1/contacts", map[string]any{"name": "Pager", "email": "pager@example.com"})
	ids := []string{
		create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "First"}),
		create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Second"}),
		create(t, api, token, "/api/v1/conversations", map[string]any{"contact_id": contactID, "subject": "Third"}),
	}
	if _, err := pool.Exec(context.Background(), `UPDATE conversations SET updated_at='2026-10-14T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))

	first := conversationIDs(t, request(t, api, http.MethodGet, "/api/v1/conversations?page=1&page_size=2", nil, token, nil))
	second := conversationIDs(t, request(t, api, http.MethodGet, "/api/v1/conversations?page=2&page_size=2", nil, token, nil))
	if len(first) != 2 || len(second) != 1 || first[0] != ids[0] || first[1] != ids[1] || second[0] != ids[2] {
		t.Fatalf("pages=%v/%v, want %v/%v", first, second, ids[:2], ids[2:])
	}
}

func conversationIDs(t *testing.T, response *httptest.ResponseRecorder) []string {
	t.Helper()
	var page struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, response, &page)
	ids := make([]string, len(page.Items))
	for index, item := range page.Items {
		ids[index] = item.ID
	}
	return ids
}
