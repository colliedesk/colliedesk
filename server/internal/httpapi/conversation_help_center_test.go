package httpapi_test

import (
	"net/http"
	"testing"
)

type conversationHelpCenterView struct {
	ID         string `json:"id"`
	Subject    string `json:"subject"`
	ChannelRef *struct {
		ID string `json:"id"`
	} `json:"channel_ref"`
	HelpCenter *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"help_center"`
}

// conversationHelpCenters maps each conversation of the inbox, by its channel's id (else its subject),
// to the name of its help center, "" for none.
func conversationHelpCenters(t *testing.T, api http.Handler, token string) map[string]string {
	t.Helper()
	var list struct {
		Items []conversationHelpCenterView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/conversations", nil, token, http.StatusOK, &list)
	names := map[string]string{}
	for _, item := range list.Items {
		key := item.Subject
		if item.ChannelRef != nil {
			key = item.ChannelRef.ID
		}
		names[key] = ""
		if item.HelpCenter != nil {
			names[key] = item.HelpCenter.Name
		}
	}
	return names
}

// A conversation a web form opens keeps the help center the form serves, the primary for a form on
// none; one the team starts came through no help center, and moving the form later leaves the
// conversations it opened where they came in.
func TestAWebFormConversationKeepsTheHelpCenterItsFormServes(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Form Shelf", "admin@form-shelf.example.com")
	enableHelpCenter(t, api, admin, "Main answers", true)
	hardware := createHelpCenter(t, api, admin, "Hardware", "hardware")

	var onHardware, onPrimary formChannel
	kbCall(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "web_form", "name": "Hardware form",
		"config": map[string]any{"allowed_origins": []string{}}, "help_center_id": hardware.ID}, admin, http.StatusCreated, &onHardware)
	kbCall(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "web_form", "name": "Main form",
		"config": map[string]any{"allowed_origins": []string{}}}, admin, http.StatusCreated, &onPrimary)
	for _, path := range []string{onHardware.Config.SubmitPath, onPrimary.Config.SubmitPath} {
		if response := postForm(api, path, `{"email":"ada@form-shelf.example.com","message":"Hello"}`, ""); response.Code != http.StatusAccepted {
			t.Fatalf("post %s: %d %s", path, response.Code, response.Body.String())
		}
	}
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Bo", "email": "bo@form-shelf.example.com"})
	started := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Started by the team"})

	got := conversationHelpCenters(t, api, admin)
	if got[onHardware.ID] != "Hardware" || got[onPrimary.ID] != "Main answers" || got["Started by the team"] != "" {
		t.Fatalf("help centers of the conversations = %v, want the Hardware form's on Hardware, the main form's on Main answers, none for the team's", got)
	}
	var one conversationHelpCenterView
	kbCall(t, api, http.MethodGet, "/api/v1/conversations/"+started, nil, admin, http.StatusOK, &one)
	if one.HelpCenter != nil {
		t.Fatalf("a conversation the team started = %+v, want no help center", one)
	}

	kbCall(t, api, http.MethodPatch, "/api/v1/channels/"+onHardware.ID, map[string]any{"help_center_id": nil}, admin, http.StatusOK, nil)
	if got := conversationHelpCenters(t, api, admin); got[onHardware.ID] != "Hardware" {
		t.Fatalf("after the form moved, its conversation is on %q, want Hardware still", got[onHardware.ID])
	}
}

// Talk to a person from a help chat opens a conversation on the help center the chat serves.
func TestAHelpChatHandoffKeepsTheHelpCenterOfTheChat(t *testing.T) {
	d := newChatDesk(t, "Chat Shelf", nil)
	d.fake.Script(chatReply("Orders ship in 2 days [#" + num(d.article.Number) + "]."))
	first := d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "When does my order ship?"})
	chatEvents(t, first)
	if response := d.call(t, http.MethodPost, "/chat/handoff", map[string]any{"email": "reader@chat-shelf.example.com"}, chatCookie(t, first)); response.Code != http.StatusCreated {
		t.Fatalf("handoff status=%d body=%s", response.Code, response.Body.String())
	}
	if got := conversationHelpCenters(t, d.api, d.admin); got[d.channel] != "Chat Shelf help" {
		t.Fatalf("help centers of the conversations = %v, want the handoff on Chat Shelf help", got)
	}
}

// A message split out of a conversation opens one on the same help center: it came in the same way.
func TestASplitConversationKeepsItsSourceHelpCenter(t *testing.T) {
	d := newChatDesk(t, "Split Shelf", nil)
	d.fake.Script(chatReply("Orders ship in 2 days [#" + num(d.article.Number) + "]."))
	first := d.call(t, http.MethodPost, "/chat/turns", map[string]any{"question": "When does my order ship?"})
	chatEvents(t, first)
	if response := d.call(t, http.MethodPost, "/chat/handoff", map[string]any{"email": "reader@split-shelf.example.com"}, chatCookie(t, first)); response.Code != http.StatusCreated {
		t.Fatalf("handoff status=%d body=%s", response.Code, response.Body.String())
	}
	var list struct {
		Items []conversationHelpCenterView `json:"items"`
	}
	kbCall(t, d.api, http.MethodGet, "/api/v1/conversations", nil, d.admin, http.StatusOK, &list)
	if len(list.Items) != 1 {
		t.Fatalf("conversations = %+v, want the handoff only", list.Items)
	}
	source := list.Items[0].ID
	var incoming string
	messages := timeline(t, d.api, d.admin, source)
	for _, message := range messages {
		if message.Kind == "incoming" {
			incoming = message.ID
		}
	}
	if incoming == "" {
		t.Fatalf("handoff messages = %+v, want a customer message", messages)
	}
	var split moveResponse
	response := request(t, d.api, http.MethodPost, "/api/v1/conversations/"+source+"/messages/move", map[string]any{"message_ids": []string{incoming},
		"to": map[string]any{"new": map[string]any{"subject": "Split off"}}}, d.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("split %s: %d %s", incoming, response.Code, response.Body.String())
	}
	decode(t, response, &split)
	if split.Move.ToConversationID == nil {
		t.Fatalf("split = %+v", split)
	}
	var created conversationHelpCenterView
	kbCall(t, d.api, http.MethodGet, "/api/v1/conversations/"+*split.Move.ToConversationID, nil, d.admin, http.StatusOK, &created)
	if created.HelpCenter == nil || created.HelpCenter.Name != "Split Shelf help" {
		t.Fatalf("split conversation help center = %+v, want Split Shelf help", created.HelpCenter)
	}
}
