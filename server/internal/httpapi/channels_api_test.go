package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	// A second mailbox is a second channel; the old route keeps answering for the first, the default.
	again := request(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "email", "name": "Billing", "config": map[string]any{"support_address": "billing@channel-desk.example.com"}}, admin, nil)
	if again.Code != http.StatusCreated {
		t.Fatalf("a second email channel: status=%d body=%s", again.Code, again.Body.String())
	}
	var second channelBody
	decode(t, again, &second)
	if items, counts := listChannels(t, api, admin); len(items) != 2 || counts["email"] != 2 {
		t.Fatalf("two email channels list as %v (counts %v)", items, counts)
	}
	legacy := request(t, api, http.MethodGet, "/api/v1/channels/email", nil, admin, nil)
	if legacy.Code != http.StatusOK || !strings.Contains(legacy.Body.String(), "help@channel-desk.example.com") {
		t.Fatalf("the old route does not see the default channel: %d %s", legacy.Code, legacy.Body.String())
	}
	if removed := request(t, api, http.MethodDelete, "/api/v1/channels/"+second.ID, nil, admin, nil); removed.Code != http.StatusNoContent {
		t.Fatalf("remove the second email channel: status=%d body=%s", removed.Code, removed.Body.String())
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
	if got := len(funnel.named("email_channel_saved")); got != 4 {
		t.Fatalf("email_channel_saved recorded %d times, want 4 (connect, a second mailbox and two new addresses, not the rename)", got)
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
	var thread struct {
		Items []struct {
			ID        string  `json:"id"`
			ChannelID *string `json:"channel_id"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+*first.ConversationID+"/messages", nil, admin, nil), &thread)
	for _, item := range thread.Items {
		if item.ID == message.ID && (item.ChannelID == nil || *item.ChannelID != email.ID) {
			t.Fatalf("a reply retried once the channel is on names channel %v, want %s", item.ChannelID, email.ID)
		}
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

type channelEventsPage struct {
	Items []struct {
		Kind      string `json:"kind"`
		ChannelID string `json:"channel_id"`
	} `json:"items"`
}

func heldReplayedEvents(t *testing.T, api http.Handler, token, channelID string) int {
	t.Helper()
	var page channelEventsPage
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels/events?channel_id="+channelID, nil, token, nil), &page)
	replayed := 0
	for _, event := range page.Items {
		if event.Kind == "held_replayed" {
			replayed++
		}
	}
	return replayed
}

// Opening held mail is the admin's decision about mail that already arrived: the ceiling on new
// conversations that guards against a flood of new mail does not cut it short.
func TestOpeningHeldMailOpensAllOfItPastTheNewConversationCeiling(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Weekend Desk", "admin@weekend-desk.example.com")
	secret := connectEmail(t, api, admin, "support@weekend-desk.example.com")
	items, _ := listChannels(t, api, admin)
	email := channelOf(t, items, "email")
	request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"enabled": false}, admin, nil)
	const held = 205
	for i := 0; i < held; i++ {
		if response := inbound(t, api, secret, mail([]string{fmt.Sprintf("From: c%d@customer.example", i), "Subject: Weekend", fmt.Sprintf("Message-ID: <weekend%d@customer.example>", i)}, "Hi.")); response.Code != http.StatusOK {
			t.Fatalf("mail %d: %d %s", i, response.Code, response.Body.String())
		}
	}
	opened := request(t, api, http.MethodPost, "/api/v1/channels/"+email.ID+"/held/replay", map[string]any{}, admin, nil)
	var replay struct {
		Results []struct {
			Status string `json:"status"`
		} `json:"results"`
	}
	decode(t, opened, &replay)
	if opened.Code != http.StatusOK || len(replay.Results) != held {
		t.Fatalf("replay of %d held: %d, %d results", held, opened.Code, len(replay.Results))
	}
	items, _ = listChannels(t, api, admin)
	if got := channelOf(t, items, "email").HeldCount; got != 0 {
		t.Fatalf("held_count after replay = %d, want 0", got)
	}
	if got := heldReplayedEvents(t, api, admin, email.ID); got != 1 {
		t.Fatalf("held_replayed events = %d, want 1", got)
	}
}

// A replay that fails part way keeps what it opened in the history, and leaves the rest held.
func TestAReplayThatFailsPartWayRecordsWhatItOpened(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Partial Desk", "admin@partial-desk.example.com")
	secret := connectEmail(t, api, admin, "support@partial-desk.example.com")
	items, _ := listChannels(t, api, admin)
	email := channelOf(t, items, "email")
	request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"enabled": false}, admin, nil)
	for i := 0; i < 4; i++ {
		inboundOK(t, api, secret, mail([]string{fmt.Sprintf("From: p%d@customer.example", i), "Subject: Partial", fmt.Sprintf("Message-ID: <partial%d@customer.example>", i)}, "Hi."))
	}
	// The third message's original can no longer be read.
	if _, err := pool.Exec(context.Background(), `UPDATE channel_inbound_events SET raw_object_key='missing/original.eml.gz'
		WHERE id=(SELECT id FROM channel_inbound_events WHERE channel_id=$1 AND state='held' ORDER BY received_at,id OFFSET 2 LIMIT 1)`, email.ID); err != nil {
		t.Fatal(err)
	}
	if failed := request(t, api, http.MethodPost, "/api/v1/channels/"+email.ID+"/held/replay", map[string]any{}, admin, nil); failed.Code == http.StatusOK {
		t.Fatalf("replay with an unreadable original answered 200: %s", failed.Body.String())
	}
	items, _ = listChannels(t, api, admin)
	if got := channelOf(t, items, "email").HeldCount; got != 2 {
		t.Fatalf("held_count after a replay that failed at the third = %d, want 2", got)
	}
	if got := heldReplayedEvents(t, api, admin, email.ID); got != 1 {
		t.Fatalf("held_replayed events = %d, want 1 for the two opened", got)
	}
}

// The channel's history shows what changed: the support address, the sender name, the inbound secret
// (never its value), and how many held messages were opened or discarded.
func TestChannelHistoryNamesWhatChangedAndHowMany(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "History Desk", "admin@history-desk.example.com")
	secret := connectEmail(t, api, admin, "support@history-desk.example.com")
	items, _ := listChannels(t, api, admin)
	email := channelOf(t, items, "email")
	request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"sender_name": "History Support"}, admin, nil)
	request(t, api, http.MethodPut, "/api/v1/channels/email", map[string]any{"support_address": "help@history-desk.example.com"}, admin, nil)
	request(t, api, http.MethodPost, "/api/v1/channels/email/secret", nil, admin, nil)
	request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"enabled": false}, admin, nil)
	for i := 0; i < 2; i++ {
		inbound(t, api, secret, mail([]string{fmt.Sprintf("From: h%d@customer.example", i), "Subject: Held", fmt.Sprintf("Message-ID: <hist%d@customer.example>", i)}, "Hi."))
	}
	var page struct {
		Items []struct {
			Kind   string   `json:"kind"`
			Fields []string `json:"fields"`
			Count  *int     `json:"count"`
		} `json:"items"`
	}
	// The secret was rotated: the old one no longer lets mail in, so the held mail is sent with the new one.
	newSecret := struct {
		InboundSecret string `json:"inbound_secret"`
	}{}
	decode(t, request(t, api, http.MethodPost, "/api/v1/channels/email/secret", nil, admin, nil), &newSecret)
	for i := 0; i < 2; i++ {
		inboundOK(t, api, newSecret.InboundSecret, mail([]string{fmt.Sprintf("From: k%d@customer.example", i), "Subject: Held", fmt.Sprintf("Message-ID: <hist-new%d@customer.example>", i)}, "Hi."))
	}
	request(t, api, http.MethodPost, "/api/v1/channels/"+email.ID+"/held/replay", map[string]any{}, admin, nil)
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels/events?channel_id="+email.ID, nil, admin, nil), &page)
	seen := map[string]bool{}
	for _, event := range page.Items {
		key := event.Kind
		for _, field := range event.Fields {
			seen[key+":"+field] = true
		}
		if event.Count != nil {
			key += fmt.Sprintf(":%d", *event.Count)
		}
		seen[key] = true
	}
	for _, want := range []string{"config_changed:sender_name", "config_changed:support_address", "credential_changed:inbound_secret", "held_replayed:2"} {
		if !seen[want] {
			t.Errorf("history has no %s: %v", want, seen)
		}
	}
}

// A web form conversation in a workspace without email keeps its live channel and says no channel
// carries its replies yet; the web app tells it apart from a removed channel by deleted.
func TestAWebFormConversationWithoutEmailIsNotARemovedChannel(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	if response := f.submit(f.slugA, `{"email":"x@example.com","message":"hi"}`, "", "203.0.113.60:1"); response.Code != http.StatusAccepted {
		t.Fatalf("submit: %d", response.Code)
	}
	list := request(t, f.api, http.MethodGet, "/api/v1/conversations", nil, f.adminA, nil)
	if !strings.Contains(list.Body.String(), `"type":"web_form","name":"Web form","enabled":true,"deleted":false,"replies_via":null`) {
		t.Fatalf("channel_ref of a web form conversation without email: %s", list.Body.String())
	}
}

// Two copies of one mail held while off open once: the history counts the messages that opened.
func TestReplayCountsOnlyTheMessagesItOpened(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Twice Desk", "admin@twice-desk.example.com")
	secret := connectEmail(t, api, admin, "support@twice-desk.example.com")
	items, _ := listChannels(t, api, admin)
	email := channelOf(t, items, "email")
	request(t, api, http.MethodPatch, "/api/v1/channels/"+email.ID, map[string]any{"enabled": false}, admin, nil)
	raw := mail([]string{"From: lee@customer.example", "Subject: Twice", "Message-ID: <twice@customer.example>"}, "Hi.")
	inboundOK(t, api, secret, raw)
	inboundOK(t, api, secret, raw)
	var replay struct {
		Results []struct {
			Status string `json:"status"`
		} `json:"results"`
	}
	decode(t, request(t, api, http.MethodPost, "/api/v1/channels/"+email.ID+"/held/replay", map[string]any{}, admin, nil), &replay)
	statuses := []string{}
	for _, result := range replay.Results {
		statuses = append(statuses, result.Status)
	}
	if strings.Join(statuses, ",") != "created,duplicate" {
		t.Fatalf("replay of two copies: %v, want created then duplicate", statuses)
	}
	var page struct {
		Items []struct {
			Kind  string `json:"kind"`
			Count *int   `json:"count"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels/events?channel_id="+email.ID, nil, admin, nil), &page)
	for _, event := range page.Items {
		if event.Kind == "held_replayed" && event.Count != nil && *event.Count != 1 {
			t.Fatalf("held_replayed count = %v, want 1", *event.Count)
		}
	}
}

// A reply in a workspace where no channel carries it yet (a web form without email) is saved with a
// delivery that says it was not sent, and is sent by Retry once email is connected.
func TestAReplyNoChannelCarriesIsSavedNotSentAndRetriedOnceEmailIsConnected(t *testing.T) {
	f := newWebFormFixture(t, nil)
	f.enable(t, f.adminA)
	if response := f.submit(f.slugA, `{"email":"visitor@example.com","message":"hi"}`, "", "203.0.113.61:1"); response.Code != http.StatusAccepted {
		t.Fatalf("submit: %d", response.Code)
	}
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, request(t, f.api, http.MethodGet, "/api/v1/conversations", nil, f.adminA, nil), &list)
	conversation := list.Items[0].ID
	var reply struct {
		ID       string `json:"id"`
		Delivery *struct {
			Status     string  `json:"status"`
			ReasonCode *string `json:"reason_code"`
			LastError  *string `json:"last_error"`
		} `json:"delivery"`
	}
	decode(t, request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages", map[string]any{"kind": "outgoing", "body": "Thanks."}, f.adminA, nil), &reply)
	if reply.Delivery == nil || reply.Delivery.Status != "failed" || reply.Delivery.ReasonCode == nil || *reply.Delivery.ReasonCode != "channel_not_connected" ||
		reply.Delivery.LastError == nil || !strings.Contains(*reply.Delivery.LastError, "connect") {
		t.Fatalf("reply with no channel to carry it: %+v", reply.Delivery)
	}
	retry := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages/"+reply.ID+"/retry", nil, f.adminA, nil)
	if retry.Code == http.StatusOK {
		t.Fatal("a reply was queued with no channel to carry it")
	}
	connectEmail(t, f.api, f.adminA, "support@form-desk.example.com")
	if retry := request(t, f.api, http.MethodPost, "/api/v1/conversations/"+conversation+"/messages/"+reply.ID+"/retry", nil, f.adminA, nil); retry.Code != http.StatusOK || !strings.Contains(retry.Body.String(), `"status":"queued"`) {
		t.Fatalf("retry once email is connected: %d %s", retry.Code, retry.Body.String())
	}
	// The reply is now one the email channel carries, as a reply queued there at once would be.
	if got := queryString(t, f.pool, `SELECT (COALESCE(m.channel_id::text,'') = COALESCE(o.channel_id::text,'-'))::text FROM messages m JOIN channel_outbox o ON o.message_id=m.id WHERE m.id=$1`, reply.ID); got != "true" {
		t.Fatalf("after Retry the reply's channel is not the one that sends it (%s)", got)
	}
}

// A channel whose setup is complete but which has received nothing yet says it is waiting for its
// first message, not working: only mail that arrived proves the forwarding works.
func TestAChannelWaitsForItsFirstMessageBeforeItIsWorking(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Waiting Desk", "admin@waiting-desk.example.com")
	secret := connectEmail(t, api, admin, "support@waiting-desk.example.com")
	before, _ := listChannels(t, api, admin)
	email := channelOf(t, before, "email")
	if email.Status.State != "waiting" || email.Status.ReasonCode == nil || *email.Status.ReasonCode != "no_message_yet" {
		t.Fatalf("a connected channel with no mail yet: %+v, want waiting / no_message_yet", email.Status)
	}
	inboundOK(t, api, secret, mail([]string{"From: first@customer.example", "To: support@waiting-desk.example.com",
		"Subject: First", "Message-ID: <first@customer.example>"}, "Hello."))
	items, _ := listChannels(t, api, admin)
	if after := channelOf(t, items, "email"); after.Status.State != "working" || after.Status.ReasonCode != nil {
		t.Fatalf("after the first message: %+v, want working", after.Status)
	}
}

// Mail the channel received but did not turn into a conversation (sent from the support address
// itself, or an automatic reply) still proves the forwarding works, so the channel stops waiting.
func TestAChannelThatReceivedOnlyDroppedMailIsWorking(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Dropped Desk", "admin@dropped-desk.example.com")
	secret := connectEmail(t, api, admin, "support@dropped-desk.example.com")
	inboundOK(t, api, secret, mail([]string{"From: support@dropped-desk.example.com", "To: support@dropped-desk.example.com",
		"Subject: Test from ourselves", "Message-ID: <self-test@dropped-desk.example.com>"}, "Does it arrive?"))
	items, _ := listChannels(t, api, admin)
	email := channelOf(t, items, "email")
	if email.Status.State != "working" || email.Status.ReasonCode != nil {
		t.Fatalf("after mail that was dropped as the desk's own address: %+v, want working", email.Status)
	}
	other := createSession(t, api, service, "Other Dropped Desk", "admin@other-dropped-desk.example.com")
	connectEmail(t, api, other, "support@other-dropped-desk.example.com")
	otherItems, _ := listChannels(t, api, other)
	if got := channelOf(t, otherItems, "email"); got.Status.State != "waiting" {
		t.Fatalf("another workspace's mail ended this workspace's wait: %+v", got.Status)
	}
}

// A server without its key ring refuses a channel secret without naming the variable to the person,
// and tells whoever runs it which one to set in its log.
func TestAChannelSecretWithoutKeysNamesTheMissingSettingOnlyInTheLog(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Secret Desk", "admin@secret-desk.example.com")
	connectEmail(t, api, admin, "support@secret-desk.example.com")
	items, _ := listChannels(t, api, admin)
	email := channelOf(t, items, "email")
	logs := captureLogs(t)
	response := request(t, api, http.MethodPut, "/api/v1/channels/"+email.ID+"/credentials", map[string]any{"name": "token", "value": "s3cret"}, admin, nil)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "secret_keys_missing") || strings.Contains(response.Body.String(), "MAILBOX_TOKEN_KEYS") {
		t.Fatalf("channel secret without keys: %d %s", response.Code, response.Body.String())
	}
	wantMissingSettingsLogged(t, logs, "channel_secrets", "MAILBOX_TOKEN_KEYS")
}

// wantMissingSettingsLogged checks that a refusal for want of server settings logged which ones, for
// whoever runs the server.
func wantMissingSettingsLogged(t *testing.T, logs *bytes.Buffer, feature, missing string) {
	t.Helper()
	for _, line := range logLines(t, logs, "server settings missing") {
		names, _ := line["missing"].([]any)
		got := make([]string, 0, len(names))
		for _, name := range names {
			got = append(got, fmt.Sprint(name))
		}
		if line["level"] == "WARN" && line["feature"] == feature && strings.Join(got, ",") == missing {
			return
		}
	}
	t.Fatalf("no warning names the missing settings %s for %s; logs:\n%s", missing, feature, logs.String())
}
