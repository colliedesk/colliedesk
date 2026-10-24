package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

type channelBody struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Summary     string `json:"summary"`
	HeldCount   int    `json:"held_count"`
	UnsentCount int    `json:"unsent_count"`
	Status      struct {
		State      string  `json:"state"`
		ReasonCode *string `json:"reason_code"`
	} `json:"status"`
	Config json.RawMessage `json:"config"`
}

func listChannels(t *testing.T, api http.Handler, token string) ([]channelBody, map[string]int) {
	t.Helper()
	response := request(t, api, http.MethodGet, "/api/v1/channels", nil, token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list channels status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Items []channelBody `json:"items"`
		Types []struct {
			Type      string `json:"type"`
			Available bool   `json:"available"`
			Count     int    `json:"count"`
		} `json:"types"`
	}
	decode(t, response, &body)
	counts := map[string]int{}
	for _, item := range body.Types {
		counts[item.Type] = item.Count
	}
	return body.Items, counts
}

func channelOf(t *testing.T, items []channelBody, kind string) channelBody {
	t.Helper()
	for _, item := range items {
		if item.Type == kind {
			return item
		}
	}
	t.Fatalf("no %s channel in %+v", kind, items)
	return channelBody{}
}

// A channel added through the new routes and one connected through the old email route are the
// same channel: listed, read, changed and removed the same way, with the same history.
func TestChannelRoutesAndTheEmailRouteShareOneChannel(t *testing.T) {
	funnel := &recordingFunnel{}
	api, service, _, _ := testAPIWithConfig(t, func(config *httpapi.Config) { config.Funnel = funnel })
	admin := createSession(t, api, service, "Channel Desk", "admin@channel-desk.example.com")
	if items, counts := listChannels(t, api, admin); len(items) != 0 || counts["email"] != 0 {
		t.Fatalf("a new workspace lists %v (counts %v), want no channel", items, counts)
	}
	created := request(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "email", "config": map[string]any{"support_address": "help@channel-desk.example.com"}}, admin, nil)
	if created.Code != http.StatusCreated {
		t.Fatalf("create email status=%d body=%s", created.Code, created.Body.String())
	}
	var email channelBody
	decode(t, created, &email)
	if email.Summary != "help@channel-desk.example.com" || !email.Enabled || !strings.Contains(string(email.Config), `"support_address":"help@channel-desk.example.com"`) {
		t.Fatalf("created email channel=%+v", email)
	}
	again := request(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "email", "config": map[string]any{"support_address": "other@channel-desk.example.com"}}, admin, nil)
	wantResponse(t, again, http.StatusConflict, "channel_type_limit")
	legacy := request(t, api, http.MethodGet, "/api/v1/channels/email", nil, admin, nil)
	if legacy.Code != http.StatusOK || !strings.Contains(legacy.Body.String(), "help@channel-desk.example.com") {
		t.Fatalf("the old route does not see the channel the new one added: %d %s", legacy.Code, legacy.Body.String())
	}
	// The same change through either route gives the same channel and counts as activation once each.
	if status, text := putChannel(t, api, admin, map[string]any{"support_address": "support@channel-desk.example.com"}); status != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", status, text)
	}
	patched := request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"config": map[string]any{"support_address": "desk@channel-desk.example.com"}}, admin, nil)
	if patched.Code != http.StatusOK {
		t.Fatalf("PATCH status=%d body=%s", patched.Code, patched.Body.String())
	}
	renamed := request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"name": "Support inbox", "config": map[string]any{"sender_name": "Desk Team"}}, admin, nil)
	if renamed.Code != http.StatusOK {
		t.Fatalf("rename status=%d body=%s", renamed.Code, renamed.Body.String())
	}
	if got := len(funnel.named("email_channel_saved")); got != 3 {
		t.Fatalf("email_channel_saved recorded %d times, want 3 (connect and two new addresses, not the rename)", got)
	}
	items, _ := listChannels(t, api, admin)
	if one := channelOf(t, items, "email"); one.ID != email.ID || one.Name != "Support inbox" || one.Summary != "desk@channel-desk.example.com" {
		t.Fatalf("after both routes: %+v", one)
	}
	invalid := request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"config": map[string]any{"support_address": "Desk <desk@x.example>"}}, admin, nil)
	wantResponse(t, invalid, http.StatusBadRequest, "invalid_request")

	// Off, then on; the history says who did what.
	off := request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"enabled": false}, admin, nil)
	var offBody channelBody
	decode(t, off, &offBody)
	if off.Code != http.StatusOK || offBody.Enabled || offBody.Status.State != "off" {
		t.Fatalf("turn off: %d %+v", off.Code, offBody)
	}
	request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"enabled": true}, admin, nil)
	events := request(t, api, http.MethodGet, "/api/v1/channels/events?channel_id="+email.ID, nil, admin, nil)
	var history struct {
		Items []struct {
			Kind  string `json:"kind"`
			Actor struct {
				Name *string `json:"name"`
			} `json:"actor"`
		} `json:"items"`
	}
	decode(t, events, &history)
	var kinds []string
	for _, item := range history.Items {
		kinds = append(kinds, item.Kind)
	}
	if strings.Join(kinds, ",") != "enabled,disabled,config_changed,renamed,config_changed,config_changed,added" {
		t.Fatalf("history=%v", kinds)
	}
	if history.Items[0].Actor.Name == nil {
		t.Fatal("the history does not name who turned the channel on")
	}

	// Removing through the new route disconnects email for the old one too.
	if removed := request(t, api, http.MethodDelete, "/api/v1/channels/"+email.ID, nil, admin, nil); removed.Code != http.StatusNoContent {
		t.Fatalf("remove status=%d body=%s", removed.Code, removed.Body.String())
	}
	gone := request(t, api, http.MethodGet, "/api/v1/channels/"+email.ID, nil, admin, nil)
	wantResponse(t, gone, http.StatusNotFound, "channel_not_found")
	if legacy := request(t, api, http.MethodGet, "/api/v1/channels/email", nil, admin, nil); strings.Contains(legacy.Body.String(), "desk@channel-desk.example.com") {
		t.Fatalf("the old route still shows the removed channel: %s", legacy.Body.String())
	}
}

// No channel route reaches another workspace's channel.
func TestChannelRoutesNeverCrossWorkspaces(t *testing.T) {
	api, service := testAPI(t)
	adminA := createSession(t, api, service, "Channel A", "admin@channel-a.example.com")
	adminB := createSession(t, api, service, "Channel B", "admin@channel-b.example.com")
	connectEmail(t, api, adminA, "support@channel-a.example.com")
	items, _ := listChannels(t, api, adminA)
	email := channelOf(t, items, "email")
	if items, _ := listChannels(t, api, adminB); len(items) != 0 {
		t.Fatalf("workspace B lists A's channels: %+v", items)
	}
	for _, call := range []struct {
		method, path string
		body         any
	}{
		{http.MethodGet, "/api/v1/channels/" + email.ID, nil},
		{http.MethodPatch, "/api/v1/channels/" + email.ID, map[string]any{"enabled": false}},
		{http.MethodDelete, "/api/v1/channels/" + email.ID, nil},
		{http.MethodPost, "/api/v1/channels/" + email.ID + "/check", nil},
		{http.MethodPost, "/api/v1/channels/" + email.ID + "/held/replay", map[string]any{}},
		{http.MethodPost, "/api/v1/channels/" + email.ID + "/held/discard", map[string]any{}},
		{http.MethodPut, "/api/v1/channels/" + email.ID + "/credentials", map[string]any{"name": "token", "value": "x"}},
	} {
		response := request(t, api, call.method, call.path, call.body, adminB, nil)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s %s from workspace B: status=%d body=%s, want 404", call.method, call.path, response.Code, response.Body.String())
		}
	}
	events := request(t, api, http.MethodGet, "/api/v1/channels/events?channel_id="+email.ID, nil, adminB, nil)
	if events.Code != http.StatusOK || strings.Contains(events.Body.String(), email.ID) {
		t.Fatalf("workspace B reads A's channel history: %d %s", events.Code, events.Body.String())
	}
	if again, _ := listChannels(t, api, adminA); channelOf(t, again, "email").Enabled != true {
		t.Fatal("workspace B turned A's channel off")
	}
}

// Mail that arrives while email is off is held, not opened; an admin opens it later as it would have
// been. A reply while off is kept with channel_off and goes once the channel is on and it is retried.
func TestEmailOffHoldsMailAndRepliesUntilItIsOn(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Off Desk", "admin@off-desk.example.com")
	secret := connectEmail(t, api, admin, "support@off-desk.example.com")
	first := inboundOK(t, api, secret, mail([]string{"From: kim@customer.example", "Subject: Before", "Message-ID: <before@customer.example>"}, "Hi."))
	items, _ := listChannels(t, api, admin)
	email := channelOf(t, items, "email")
	request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"enabled": false}, admin, nil)
	held := inboundOK(t, api, secret, mail([]string{"From: lee@customer.example", "Subject: While off", "Message-ID: <off@customer.example>"}, "Anyone?"))
	if held.Status != "held" || held.ConversationID != nil {
		t.Fatalf("mail while off: %+v, want held without a conversation", held)
	}
	items, _ = listChannels(t, api, admin)
	if got := channelOf(t, items, "email"); got.HeldCount != 1 {
		t.Fatalf("held_count=%d, want 1", got.HeldCount)
	}
	reply := request(t, api, http.MethodPost, "/api/v1/conversations/"+*first.ConversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Saved while off."}, admin, nil)
	var message struct {
		ID       string `json:"id"`
		Delivery struct {
			Status     string  `json:"status"`
			ReasonCode *string `json:"reason_code"`
		} `json:"delivery"`
	}
	decode(t, reply, &message)
	if message.Delivery.Status != "failed" || message.Delivery.ReasonCode == nil || *message.Delivery.ReasonCode != "channel_off" {
		t.Fatalf("reply while off: %+v, want failed channel_off", message.Delivery)
	}
	conversation := request(t, api, http.MethodGet, "/api/v1/conversations/"+*first.ConversationID, nil, admin, nil)
	if !strings.Contains(conversation.Body.String(), `"replies_via":{"id":"`+email.ID+`"`) || !strings.Contains(conversation.Body.String(), `"enabled":false`) {
		t.Fatalf("conversation channel_ref does not show the channel is off: %s", conversation.Body.String())
	}
	retry := request(t, api, http.MethodPost, "/api/v1/conversations/"+*first.ConversationID+"/messages/"+message.ID+"/retry", nil, admin, nil)
	if retry.Code == http.StatusOK {
		t.Fatal("a reply was requeued while its channel is off")
	}
	request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"enabled": true}, admin, nil)
	if retry := request(t, api, http.MethodPost, "/api/v1/conversations/"+*first.ConversationID+"/messages/"+message.ID+"/retry", nil, admin, nil); retry.Code != http.StatusOK {
		t.Fatalf("retry once on: %d %s", retry.Code, retry.Body.String())
	}
	opened := request(t, api, http.MethodPost, "/api/v1/channels/"+email.ID+"/held/replay", map[string]any{}, admin, nil)
	var replay struct {
		Results []struct {
			Status         string  `json:"status"`
			ConversationID *string `json:"conversation_id"`
		} `json:"results"`
	}
	decode(t, opened, &replay)
	if opened.Code != http.StatusOK || len(replay.Results) != 1 || replay.Results[0].ConversationID == nil {
		t.Fatalf("replay: %d %+v", opened.Code, replay)
	}
	timeline := request(t, api, http.MethodGet, "/api/v1/conversations/"+*replay.Results[0].ConversationID+"/messages", nil, admin, nil)
	if !strings.Contains(timeline.Body.String(), "Anyone?") || !strings.Contains(timeline.Body.String(), "original-message.eml.gz") {
		t.Fatalf("the held mail opened without its text or original: %s", timeline.Body.String())
	}
}

// The web form is a channel too: the old settings route and the channel routes turn the same form on
// and off, and a form that is off answers like one that does not exist.
func TestWebFormIsAChannelBothRoutesTurnOnAndOff(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	items, _ := listChannels(t, f.api, f.adminA)
	form := channelOf(t, items, "web_form")
	if !form.Enabled {
		t.Fatalf("form after PUT on: %+v", form)
	}
	request(t, f.api, http.MethodPatch, "/api/v1/channels/"+form.ID, map[string]any{"enabled": false}, f.adminA, nil)
	legacy := request(t, f.api, http.MethodGet, "/api/v1/channels/web-form", nil, f.adminA, nil)
	if !strings.Contains(legacy.Body.String(), `"enabled":false`) {
		t.Fatalf("old route after PATCH off: %s", legacy.Body.String())
	}
	if response := f.submit(f.slugA, `{"email":"x@example.com"}`, "", "203.0.113.50:1"); response.Code != http.StatusNotFound {
		t.Fatalf("submit to a form that is off: %d, want 404", response.Code)
	}
	f.enable(t, f.adminA)
	if response := f.submit(f.slugA, `{"email":"x@example.com"}`, "", "203.0.113.51:1"); response.Code != http.StatusAccepted {
		t.Fatalf("submit after turning on again: %d", response.Code)
	}
}

// The export holds every channel and what a channel kept that is not a message, with the raw mail of
// a held message in the archive; never a channel secret.
func TestExportHoldsChannelsAndHeldMail(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "Export Channels", "admin@export-channels.example.com")
	secret := connectEmail(t, api, admin, "support@export-channels.example.com")
	items, _ := listChannels(t, api, admin)
	email := channelOf(t, items, "email")
	request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"enabled": false}, admin, nil)
	if held := inboundOK(t, api, secret, mail([]string{"From: zoe@customer.example", "Subject: Kept", "Message-ID: <kept@customer.example>"}, "Keep me.")); held.Status != "held" {
		t.Fatalf("mail while off: %+v", held)
	}
	response := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d body=%s", response.Code, response.Body.String())
	}
	files := unzip(t, response.Body.Bytes())
	if !strings.Contains(string(files["channels.ndjson"]), `"id":"`+email.ID+`"`) || !strings.Contains(string(files["channels.ndjson"]), `"support_address":"support@export-channels.example.com"`) {
		t.Fatalf("channels.ndjson=%s", files["channels.ndjson"])
	}
	if !strings.Contains(string(files["channel-events.ndjson"]), `"kind":"disabled"`) {
		t.Fatalf("channel-events.ndjson=%s", files["channel-events.ndjson"])
	}
	inbound := string(files["channel-inbound-events.ndjson"])
	var event struct {
		State   string  `json:"state"`
		RawPath *string `json:"raw_path"`
	}
	if err := json.Unmarshal([]byte(strings.SplitN(inbound, "\n", 2)[0]), &event); err != nil || event.State != "held" || event.RawPath == nil {
		t.Fatalf("channel-inbound-events.ndjson=%s (%v)", inbound, err)
	}
	if len(files[*event.RawPath]) == 0 {
		t.Fatalf("the held mail's original is not in the archive at %s", *event.RawPath)
	}
}

// A customer known only on a channel has no email: listed, read and written to like any other, and a
// reply to them is kept with no_recipient instead of failing the request.
func TestContactWithoutEmailIsListedReadAndAnswered(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Chat Desk", "admin@chat-desk.example.com")
	connectEmail(t, api, admin, "support@chat-desk.example.com")
	items, _ := listChannels(t, api, admin)
	email := channelOf(t, items, "email")
	var contactID, conversationID string
	if err := pool.QueryRow(t.Context(), `WITH k AS (INSERT INTO contacts(workspace_id,name) SELECT workspace_id,'Minh' FROM channels WHERE id=$1 RETURNING id,workspace_id),
		i AS (INSERT INTO contact_identities(workspace_id,contact_id,channel_id,kind,value,display_name) SELECT workspace_id,id,$1,'chat_user','4410233','Minh T.' FROM k RETURNING 1),
		c AS (INSERT INTO conversations(workspace_id,contact_id,subject,channel,channel_id) SELECT workspace_id,id,'Chat','email',$1 FROM k RETURNING id)
		SELECT (SELECT id::text FROM k),(SELECT id::text FROM c)`, email.ID).Scan(&contactID, &conversationID); err != nil {
		t.Fatal(err)
	}
	list := request(t, api, http.MethodGet, "/api/v1/contacts?search=Minh", nil, admin, nil)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"email":null`) || !strings.Contains(list.Body.String(), `"value":"4410233"`) {
		t.Fatalf("contacts: %d %s", list.Code, list.Body.String())
	}
	if got := request(t, api, http.MethodGet, "/api/v1/contacts/"+contactID, nil, admin, nil); got.Code != http.StatusOK {
		t.Fatalf("contact: %d %s", got.Code, got.Body.String())
	}
	reply := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "outgoing", "body": "Hello", "cc": []string{"boss@chat.example"}}, admin, nil)
	if reply.Code != http.StatusCreated || !strings.Contains(reply.Body.String(), `"reason_code":"no_recipient"`) {
		t.Fatalf("reply to a contact without email: %d %s", reply.Code, reply.Body.String())
	}
}

// A channel that holds messages cannot be removed: they would be hidden from the app and still count
// toward storage. Both ways to remove email refuse until they are opened or discarded.
func TestAChannelThatHoldsMessagesIsNotRemoved(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Held Desk", "admin@held-desk.example.com")
	secret := connectEmail(t, api, admin, "support@held-desk.example.com")
	items, _ := listChannels(t, api, admin)
	email := channelOf(t, items, "email")
	request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"enabled": false}, admin, nil)
	inboundOK(t, api, secret, mail([]string{"From: lee@customer.example", "Subject: While off", "Message-ID: <held-remove@customer.example>"}, "Anyone?"))
	for _, path := range []string{"/api/v1/channels/" + email.ID, "/api/v1/channels/email"} {
		refused := request(t, api, http.MethodDelete, path, nil, admin, nil)
		var body struct {
			Error struct {
				Code      string `json:"code"`
				Message   string `json:"message"`
				HeldCount int    `json:"held_count"`
			} `json:"error"`
		}
		decode(t, refused, &body)
		if refused.Code != http.StatusConflict || body.Error.Code != "channel_has_held" || body.Error.HeldCount != 1 || !strings.Contains(body.Error.Message, "1 message") {
			t.Fatalf("DELETE %s with a held message: %d %s", path, refused.Code, refused.Body.String())
		}
		items, _ = listChannels(t, api, admin)
		if got := channelOf(t, items, "email"); got.ID != email.ID || got.HeldCount != 1 {
			t.Fatalf("after a refused DELETE %s: %+v, want the same channel holding 1", path, got)
		}
	}
	if discarded := request(t, api, http.MethodPost, "/api/v1/channels/"+email.ID+"/held/discard", map[string]any{}, admin, nil); discarded.Code != http.StatusOK {
		t.Fatalf("discard: %d %s", discarded.Code, discarded.Body.String())
	}
	if removed := request(t, api, http.MethodDelete, "/api/v1/channels/email", nil, admin, nil); removed.Code != http.StatusNoContent {
		t.Fatalf("disconnect once nothing is held: %d %s", removed.Code, removed.Body.String())
	}
}
