package httpapi_test

import (
	"net/http"
	"reflect"
	"testing"
)

// The people a reply was copied to are part of the record: an export without them loses data.
func TestExportKeepsTheCcListOfEveryReply(t *testing.T) {
	api, service, _ := testAPIWithPool(t)
	admin := createSession(t, api, service, "Export Cc", "admin@export-cc.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ann", "email": "ann@customer.example"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Copied"})
	copied := create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages",
		map[string]any{"kind": "outgoing", "body": "Copying your team.", "cc": []string{"boss@customer.example", "ops@customer.example"}})
	plain := create(t, api, admin, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Just you."})

	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	lines := ndjsonLines(t, unzip(t, response.Body.Bytes())["conversations.ndjson"])
	found := map[string]any{}
	for _, line := range lines {
		for _, item := range line["messages"].([]any) {
			message := item.(map[string]any)
			value, present := message["cc"]
			if !present {
				t.Fatalf("message %v has no cc key", message["id"])
			}
			found[message["id"].(string)] = value
		}
	}
	if want := []any{"boss@customer.example", "ops@customer.example"}; !reflect.DeepEqual(found[copied], want) {
		t.Fatalf("copied reply cc=%v, want %v", found[copied], want)
	}
	if found[plain] != nil {
		t.Fatalf("reply without copies cc=%v, want null", found[plain])
	}
}
