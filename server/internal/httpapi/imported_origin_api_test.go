package httpapi_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// A conversation and a message whose origin carries none of the usual parts (an import from a bundle,
// an older row) are still read as the contract says: lists are lists, never null, so one odd origin never
// keeps the inbox from loading.
func TestAnOriginWithoutItsListsIsReadWithEmptyLists(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Origin Desk", "admin@origin.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@origin.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Imported"})
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE conversations SET origin='{"import":{"source":"bundle","bundle":"b","id":"t1"},"reference":"R-1"}' WHERE id=$1`, conversation); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO messages(workspace_id,conversation_id,kind,body,origin) SELECT workspace_id,id,'incoming','Hello','{"import":{"source":"bundle","bundle":"b","id":"m1"}}'
		FROM conversations WHERE id=$1`, conversation); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/conversations", "/api/v1/conversations/" + conversation, "/api/v1/conversations/" + conversation + "/messages"} {
		response := request(t, api, http.MethodGet, path, nil, admin, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, response.Code, response.Body.String())
		}
		validateContractResponse(t, newRequest(t, http.MethodGet, path, nil, admin, nil), response)
		if body := response.Body.String(); strings.Contains(body, `"cc":null`) || strings.Contains(body, `"forwarded_to":null`) || strings.Contains(body, `"to":null`) || strings.Contains(body, `"bcc":null`) {
			t.Fatalf("GET %s: a list is null: %s", path, body)
		}
	}
}
