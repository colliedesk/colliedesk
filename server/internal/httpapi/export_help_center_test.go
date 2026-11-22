package httpapi_test

import (
	"context"
	"net/http"
	"testing"
)

// The export says which help center a channel serves and which one a conversation came in through;
// null is the primary for a channel and none for a conversation (email).
func TestExportNamesTheHelpCenterOfAChannelAndOfAConversation(t *testing.T) {
	pool := testPool(t)
	api, service, _, _ := testAPIOnPool(t, pool, nil)
	admin := createSession(t, api, service, "Export Shelf", "admin@export-shelf.example.com")
	enableHelpCenter(t, api, admin, "Main answers", true)
	hardware := createHelpCenter(t, api, admin, "Hardware", "hardware")

	var form, chat channelHelpCenterView
	kbCall(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "web_form", "name": "Hardware form",
		"config": map[string]any{"allowed_origins": []string{}}, "help_center_id": hardware.ID}, admin, http.StatusCreated, &form)
	kbCall(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "help_chat", "config": map[string]any{"greeting": "Hi!"}}, admin, http.StatusCreated, &chat)

	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@export-shelf.example.com"})
	onHardware := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Router"})
	plain := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Invoice"})
	if _, err := pool.Exec(context.Background(), `UPDATE conversations SET help_center_id=$2::uuid WHERE id=$1::uuid`, onHardware, hardware.ID); err != nil {
		t.Fatal(err)
	}

	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d", response.Code)
	}
	entries := unzip(t, response.Body.Bytes())
	served := map[string]any{}
	for _, channel := range ndjsonLines(t, entries["channels.ndjson"]) {
		if _, ok := channel["help_center_id"]; !ok {
			t.Fatalf("channel %v has no help_center_id", channel)
		}
		served[channel["id"].(string)] = channel["help_center_id"]
	}
	if served[form.ID] != hardware.ID || served[chat.ID] != nil {
		t.Fatalf("exported channel help centers = %v, want the form on %s and the chat on the primary (null)", served, hardware.ID)
	}
	came := map[string]any{}
	for _, conversation := range ndjsonLines(t, entries["conversations.ndjson"]) {
		if _, ok := conversation["help_center_id"]; !ok {
			t.Fatalf("conversation %v has no help_center_id", conversation["id"])
		}
		came[conversation["id"].(string)] = conversation["help_center_id"]
	}
	if came[onHardware] != hardware.ID || came[plain] != nil {
		t.Fatalf("exported conversation help centers = %v, want %s on Hardware and none on the other", came, onHardware)
	}
}
