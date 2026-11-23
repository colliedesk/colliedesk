package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

type channelHelpCenterView struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	HelpCenterID   *string `json:"help_center_id"`
	HelpCenterName *string `json:"help_center_name"`
}

func errorField(t *testing.T, body []byte, field string) string {
	t.Helper()
	var answer struct {
		Error struct {
			Fields map[string]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &answer); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return answer.Error.Fields[field]
}

// A web form or help chat serves the help center it names, which the channel shows by id and by
// name; null serves the primary again. Only those two types name one, and only a live help center of
// the workspace.
func TestAWebFormOrHelpChatServesTheHelpCenterItNames(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Channel Shelf", "admin@channel-shelf.example.com")
	enableHelpCenter(t, api, admin, "Main answers", true)
	primary := listHelpCenters(t, api, admin).HelpCenters[0]
	hardware := createHelpCenter(t, api, admin, "Hardware", "hardware")

	var form channelHelpCenterView
	kbCall(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "web_form", "name": "Hardware form",
		"config": map[string]any{"allowed_origins": []string{}}, "help_center_id": hardware.ID}, admin, http.StatusCreated, &form)
	if form.HelpCenterID == nil || *form.HelpCenterID != hardware.ID || form.HelpCenterName == nil || *form.HelpCenterName != "Hardware" {
		t.Fatalf("created form = %+v, want it to serve Hardware", form)
	}
	var read channelHelpCenterView
	kbCall(t, api, http.MethodGet, "/api/v1/channels/"+form.ID, nil, admin, http.StatusOK, &read)
	if read.HelpCenterID == nil || *read.HelpCenterID != hardware.ID {
		t.Fatalf("read form = %+v", read)
	}

	var chat channelHelpCenterView
	kbCall(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "help_chat", "config": map[string]any{"greeting": "Hi!"}}, admin, http.StatusCreated, &chat)
	if chat.HelpCenterID != nil || chat.HelpCenterName == nil || *chat.HelpCenterName != primary.Name {
		t.Fatalf("new chat = %+v, want the primary (%s)", chat, primary.Name)
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/channels/"+chat.ID, map[string]any{"help_center_id": hardware.ID}, admin, http.StatusOK, &chat)
	if chat.HelpCenterID == nil || *chat.HelpCenterID != hardware.ID {
		t.Fatalf("moved chat = %+v", chat)
	}
	kbCall(t, api, http.MethodPatch, "/api/v1/channels/"+chat.ID, map[string]any{"help_center_id": nil}, admin, http.StatusOK, &chat)
	if chat.HelpCenterID != nil || chat.HelpCenterName == nil || *chat.HelpCenterName != primary.Name {
		t.Fatalf("chat set back = %+v, want the primary", chat)
	}
	// Naming the primary by its id is the same as null.
	kbCall(t, api, http.MethodPatch, "/api/v1/channels/"+chat.ID, map[string]any{"help_center_id": primary.ID}, admin, http.StatusOK, &chat)
	if chat.HelpCenterID != nil {
		t.Fatalf("chat on the primary by id = %+v, want null", chat)
	}

	var email channelHelpCenterView
	kbCall(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "email", "name": "Billing",
		"config": map[string]any{"support_address": "billing@channel-shelf.example.com"}}, admin, http.StatusCreated, &email)
	if email.HelpCenterID != nil || email.HelpCenterName != nil {
		t.Errorf("email channel = %+v, want no help center", email)
	}
	refused := kbCall(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"help_center_id": hardware.ID}, admin, http.StatusBadRequest, nil)
	if message := errorField(t, refused.Body.Bytes(), "help_center_id"); !strings.Contains(message, "help chat or a web form") {
		t.Errorf("email naming a help center: %s", refused.Body.String())
	}

	other := createSession(t, api, service, "Other Shelf", "admin@other-shelf.example.com")
	foreign := createHelpCenter(t, api, other, "Theirs", "theirs")
	for _, id := range []string{foreign.ID, "00000000-0000-4000-8000-000000000000", "not-an-id"} {
		unknown := kbCall(t, api, http.MethodPatch, "/api/v1/channels/"+form.ID, map[string]any{"help_center_id": id}, admin, http.StatusBadRequest, nil)
		if message := errorField(t, unknown.Body.Bytes(), "help_center_id"); message == "" {
			t.Errorf("help center %q: %s", id, unknown.Body.String())
		}
	}
	kbCall(t, api, http.MethodGet, "/api/v1/channels/"+form.ID, nil, admin, http.StatusOK, &read)
	if read.HelpCenterID == nil || *read.HelpCenterID != hardware.ID {
		t.Fatalf("after refused changes = %+v, want Hardware kept", read)
	}
}

// A help center a live channel serves cannot be deleted: the refusal names the channels, so the
// person or agent knows what to move first. Once the channel serves another, the delete goes through.
func TestDeletingAHelpCenterAChannelServesIsRefusedAndNamesTheChannel(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Delete Shelf", "admin@delete-shelf.example.com")
	enableHelpCenter(t, api, admin, "Main answers", true)
	hardware := createHelpCenter(t, api, admin, "Hardware", "hardware")
	var form, chat channelHelpCenterView
	kbCall(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "web_form", "name": "Hardware form",
		"config": map[string]any{"allowed_origins": []string{}}, "help_center_id": hardware.ID}, admin, http.StatusCreated, &form)
	kbCall(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "help_chat", "name": "Hardware chat",
		"config": map[string]any{"greeting": "Hi!"}, "help_center_id": hardware.ID, "enabled": false}, admin, http.StatusCreated, &chat)

	refused := kbCall(t, api, http.MethodDelete, "/api/v1/help-centers/"+hardware.ID, nil, admin, http.StatusConflict, nil)
	var answer struct {
		Error struct {
			Code     string `json:"code"`
			Message  string `json:"message"`
			Channels []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"channels"`
		} `json:"error"`
	}
	if err := json.Unmarshal(refused.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Error.Code != "help_center_in_use" || !strings.Contains(answer.Error.Message, "Hardware form") ||
		!strings.Contains(answer.Error.Message, "Hardware chat") || len(answer.Error.Channels) != 2 {
		t.Fatalf("refusal = %s, want help_center_in_use naming both channels", refused.Body.String())
	}
	kbCall(t, api, http.MethodGet, "/api/v1/help-centers/"+hardware.ID, nil, admin, http.StatusOK, nil)

	kbCall(t, api, http.MethodPatch, "/api/v1/channels/"+form.ID, map[string]any{"help_center_id": nil}, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodDelete, "/api/v1/channels/"+chat.ID, nil, admin, http.StatusNoContent, nil)
	kbCall(t, api, http.MethodDelete, "/api/v1/help-centers/"+hardware.ID, nil, admin, http.StatusNoContent, nil)
}
