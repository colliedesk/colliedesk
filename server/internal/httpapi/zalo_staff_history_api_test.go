package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"
)

// Adding a Zalo team member is one setting changed in the channel's history, with how many conversations
// only they wrote in were closed as its count, never a setting of its own.
func TestAddingZaloStaffShowsTheSettingAndHowManyConversationsClosed(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Staff History Desk", "admin@staff-history.example.com")
	id := create(t, api, admin, "/api/v1/channels", map[string]any{"type": "zalo", "config": map[string]any{"oa_id": "4440021", "app_id": "700021"}})
	request(t, api, http.MethodPatch, "/api/v1/channels/"+id, map[string]any{"config": map[string]any{"staff_user_ids": []string{"5550021"}}}, admin, nil)
	var page struct {
		Items []struct {
			Kind   string   `json:"kind"`
			Fields []string `json:"fields"`
			Count  *int     `json:"count"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels/events?channel_id="+id, nil, admin, nil), &page)
	for _, event := range page.Items {
		if event.Kind != "config_changed" {
			continue
		}
		if fmt.Sprint(event.Fields) != "[staff_user_ids]" || event.Count == nil || *event.Count != 0 {
			got := "null"
			if event.Count != nil {
				got = fmt.Sprint(*event.Count)
			}
			t.Fatalf("history = fields %v count %s, want fields [staff_user_ids] count 0", event.Fields, got)
		}
		return
	}
	t.Fatalf("history has no config_changed: %+v", page.Items)
}
