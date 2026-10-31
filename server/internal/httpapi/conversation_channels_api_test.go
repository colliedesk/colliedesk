package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

type routedConversation struct {
	ID         string `json:"id"`
	ChannelRef *struct {
		ID         string `json:"id"`
		RepliesVia *struct {
			ID        string `json:"id"`
			Type      string `json:"type"`
			InsteadOf *struct {
				ID string `json:"id"`
			} `json:"instead_of"`
		} `json:"replies_via"`
	} `json:"channel_ref"`
}

func conversationOf(t *testing.T, api http.Handler, token, id string) routedConversation {
	t.Helper()
	var conversation routedConversation
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations/"+id, nil, token, nil), &conversation)
	return conversation
}

// deliveryChannel is the channel the delivery of the conversation's newest reply leaves from.
func deliveryChannel(t *testing.T, pool *pgxpool.Pool, conversationID string) string {
	t.Helper()
	return queryString(t, pool, `SELECT o.channel_id::text FROM channel_outbox o JOIN messages m ON m.workspace_id=o.workspace_id AND m.id=o.message_id
		WHERE m.conversation_id=$1 ORDER BY o.created_at DESC,o.id DESC LIMIT 1`, conversationID)
}

func reply(t *testing.T, api http.Handler, token, conversationID, body string) {
	t.Helper()
	response := request(t, api, http.MethodPost, "/api/v1/conversations/"+conversationID+"/messages", map[string]any{"kind": "outgoing", "body": body}, token, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("reply: %d %s", response.Code, response.Body.String())
	}
}

// A conversation the team starts leaves from the channel chosen and keeps it, whatever becomes the
// default later; without a choice it leaves from the default. A choice it cannot start from is refused.
func TestAConversationStartsFromTheChannelChosenAndKeepsIt(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Start From", "admin@start-from.example.com")
	connectEmail(t, api, admin, "support@start-from.example.com")
	support := emailChannelOf(t, api, admin, "").Channel.ChannelID
	billing := addEmailChannel(t, api, admin, "billing@start-from.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Olive", "email": "olive@customer.example"})
	start := func(body map[string]any) *routedConversation {
		t.Helper()
		body["contact_id"], body["subject"], body["body"] = contact, "Hello", "First words."
		response := request(t, api, http.MethodPost, "/api/v1/conversations", body, admin, nil)
		if response.Code != http.StatusCreated {
			t.Fatalf("start %v: %d %s", body, response.Code, response.Body.String())
		}
		validateContractResponse(t, newRequest(t, http.MethodPost, "/api/v1/conversations", body, admin, nil), response)
		var conversation routedConversation
		decode(t, response, &conversation)
		return &conversation
	}
	fromBilling := start(map[string]any{"channel_id": billing})
	fromDefault := start(map[string]any{})
	// The web app reads how replies leave from the type of the channel they leave through.
	if via := conversationOf(t, api, admin, fromBilling.ID).ChannelRef.RepliesVia; via == nil || via.ID != billing || via.Type != "email" {
		t.Fatalf("replies_via of a conversation started from billing: %+v, want billing by email", via)
	}
	if got := deliveryChannel(t, pool, fromBilling.ID); got != billing {
		t.Fatalf("started from billing, the reply leaves from %s", got)
	}
	if got := deliveryChannel(t, pool, fromDefault.ID); got != support {
		t.Fatalf("started without a channel, the reply leaves from %s, want the default %s", got, support)
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/channels/"+billing, map[string]any{"is_default": true}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("make billing the default: %d", response.Code)
	}
	reply(t, api, admin, fromDefault.ID, "Second words.")
	if got := deliveryChannel(t, pool, fromDefault.ID); got != support {
		t.Fatalf("a new default moved an older conversation's replies to %s", got)
	}

	request(t, api, http.MethodPut, "/api/v1/channels/web-form", map[string]any{"enabled": true, "allowed_origins": []string{}}, admin, nil)
	form := ""
	var list struct {
		Items []channelBody `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/channels", nil, admin, nil), &list)
	for _, item := range list.Items {
		if item.Type == "web_form" {
			form = item.ID
		}
	}
	other := createSession(t, api, service, "Start Elsewhere", "admin@start-elsewhere.example.com")
	connectEmail(t, api, other, "support@start-elsewhere.example.com")
	foreign := emailChannelOf(t, api, other, "").Channel.ChannelID
	for _, item := range []struct {
		channel string
		status  int
		code    string
	}{{foreign, http.StatusNotFound, "channel_not_found"}, {"00000000-0000-4000-8000-000000000000", http.StatusNotFound, "channel_not_found"},
		{form, http.StatusBadRequest, "channel_cannot_send"}} {
		body := map[string]any{"contact_id": contact, "subject": "Refused", "body": "x", "channel_id": item.channel}
		response := request(t, api, http.MethodPost, "/api/v1/conversations", body, admin, nil)
		if response.Code != item.status || !strings.Contains(response.Body.String(), item.code) {
			t.Errorf("start from %s: %d %s, want %d %s", item.channel, response.Code, response.Body.String(), item.status, item.code)
		}
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/channels/"+billing, map[string]any{"enabled": false}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("turn billing off: %d", response.Code)
	}
	for _, body := range []map[string]any{{"channel_id": billing}, {}} {
		body["contact_id"], body["subject"], body["body"] = contact, "Off", "x"
		if response := request(t, api, http.MethodPost, "/api/v1/conversations", body, admin, nil); response.Code != http.StatusConflict ||
			!strings.Contains(response.Body.String(), "channel_off") {
			t.Errorf("start from an off channel %v: %d %s, want 409 channel_off", body["channel_id"], response.Code, response.Body.String())
		}
	}
	if count := queryString(t, pool, `SELECT count(*)::text FROM conversations WHERE subject IN ('Refused','Off')`); count != "0" {
		t.Fatalf("refused starts stored %s conversations", count)
	}
}

// A web form conversation's replies leave by email: the first reply pins the default mailbox, which
// they keep; once it is removed the new default stands in, and the conversation says for which.
func TestAFormConversationKeepsTheMailboxOfItsFirstReply(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Form Replies", "admin@form-replies.example.com")
	connectEmail(t, api, admin, "support@form-replies.example.com")
	support := emailChannelOf(t, api, admin, "").Channel.ChannelID
	billing := addEmailChannel(t, api, admin, "billing@form-replies.example.com")
	form := addWebForm(t, api, admin, "Contact form", []string{})
	if response := postForm(api, "/api/v1/public/web-forms/"+form.ID, `{"email":"visitor@example.com","message":"Hi"}`, ""); response.Code != http.StatusAccepted {
		t.Fatalf("submit: %d", response.Code)
	}
	conversation := ""
	for id := range conversationChannels(t, api, admin) {
		conversation = id
	}
	reply(t, api, admin, conversation, "First.")
	if got := deliveryChannel(t, pool, conversation); got != support {
		t.Fatalf("first reply left from %s, want the default %s", got, support)
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/channels/"+billing, map[string]any{"is_default": true}, admin, nil); response.Code != http.StatusOK {
		t.Fatal("make billing the default")
	}
	reply(t, api, admin, conversation, "Second.")
	if got := deliveryChannel(t, pool, conversation); got != support {
		t.Fatalf("second reply left from %s, want the pinned %s", got, support)
	}
	if response := request(t, api, http.MethodDelete, "/api/v1/channels/"+support, nil, admin, nil); response.Code != http.StatusNoContent {
		t.Fatalf("remove support: %d", response.Code)
	}
	reply(t, api, admin, conversation, "Third.")
	if got := deliveryChannel(t, pool, conversation); got != billing {
		t.Fatalf("after the pinned mailbox went, the reply left from %s, want the default %s", got, billing)
	}
	view := conversationOf(t, api, admin, conversation)
	if view.ChannelRef == nil || view.ChannelRef.ID != form.ID || view.ChannelRef.RepliesVia == nil || view.ChannelRef.RepliesVia.ID != billing ||
		view.ChannelRef.RepliesVia.InsteadOf == nil || view.ChannelRef.RepliesVia.InsteadOf.ID != support {
		t.Fatalf("channel_ref = %+v", view.ChannelRef)
	}
}

// The inbox and saved views filter by channel: a conversation that came through it or whose replies
// leave from it. A channel of another workspace matches nothing.
func TestConversationsFilterByChannel(t *testing.T) {
	api, service, _, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Filter Boxes", "admin@filter-boxes.example.com")
	connectEmail(t, api, admin, "support@filter-boxes.example.com")
	support := emailChannelOf(t, api, admin, "").Channel.ChannelID
	billing := addEmailChannel(t, api, admin, "billing@filter-boxes.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Olive", "email": "olive@customer.example"})
	fromBilling := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Invoice", "body": "x", "channel_id": billing})
	fromSupport := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Help", "body": "x"})
	listed := func(token, query string) []string {
		t.Helper()
		response := request(t, api, http.MethodGet, "/api/v1/conversations?"+query, nil, token, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("?%s: %d %s", query, response.Code, response.Body.String())
		}
		var page struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		}
		decode(t, response, &page)
		ids := []string{}
		for _, item := range page.Items {
			ids = append(ids, item.ID)
		}
		return ids
	}
	if got := listed(admin, "channel_id="+billing); len(got) != 1 || got[0] != fromBilling {
		t.Fatalf("channel_id=billing lists %v, want [%s]", got, fromBilling)
	}
	if got := listed(admin, "channel_id="+billing+","+support); len(got) != 2 {
		t.Fatalf("both channels list %v", got)
	}
	_ = fromSupport
	other := createSession(t, api, service, "Filter Elsewhere", "admin@filter-elsewhere.example.com")
	if got := listed(other, "channel_id="+billing); len(got) != 0 {
		t.Fatalf("another workspace's channel lists %v", got)
	}
	if response := request(t, api, http.MethodGet, "/api/v1/conversations?channel_id=nope", nil, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("a malformed channel id: %d", response.Code)
	}
	view := request(t, api, http.MethodPost, "/api/v1/saved-views", map[string]any{"name": "Billing", "scope": "personal", "filters": map[string]any{"channel_id": []string{billing}}}, admin, nil)
	if view.Code != http.StatusCreated || !strings.Contains(view.Body.String(), billing) {
		t.Fatalf("saved view with channel_id: %d %s", view.Code, view.Body.String())
	}
}

// An imported conversation's replies leave from the email channel of the mailbox it was sent to at the
// source, when the workspace has one, and keep it; one whose mailbox it has not leaves from the default.
func TestAnImportedConversationRepliesFromItsSourceMailbox(t *testing.T) {
	api, service, pool, _ := testAPIWithConfig(t, nil)
	admin := createSession(t, api, service, "Imported Boxes", "admin@imported-boxes.example.com")
	connectEmail(t, api, admin, "support@imported-boxes.example.com")
	support := emailChannelOf(t, api, admin, "").Channel.ChannelID
	billing := addEmailChannel(t, api, admin, "billing@imported-boxes.example.com")
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Olive", "email": "olive@customer.example"})
	imported := func(mailboxes string) string {
		t.Helper()
		return queryString(t, pool, `INSERT INTO conversations(workspace_id,contact_id,subject,channel,origin)
			SELECT workspace_id,id,'Imported','imported',jsonb_build_object('system','freshdesk','external_id','1','mailboxes',$2::jsonb)
			FROM contacts WHERE id=$1 RETURNING id::text`, contact, mailboxes)
	}
	fromBilling := imported(`["Billing+Desk@Imported-Boxes.example.com", "support@imported-boxes.example.com"]`)
	unknown := imported(`["sales@elsewhere.example.com"]`)
	if via := conversationOf(t, api, admin, fromBilling).ChannelRef; via == nil || via.RepliesVia == nil || via.RepliesVia.ID != billing {
		t.Fatalf("before any reply, channel_ref = %+v, want replies via billing", via)
	}
	// The list reads the mailboxes once for the page and says the same of every row.
	var page struct {
		Items []struct {
			ID         string `json:"id"`
			ChannelRef *struct {
				RepliesVia *struct {
					ID string `json:"id"`
				} `json:"replies_via"`
			} `json:"channel_ref"`
		} `json:"items"`
	}
	decode(t, request(t, api, http.MethodGet, "/api/v1/conversations?page_size=100", nil, admin, nil), &page)
	listed := map[string]string{}
	for _, item := range page.Items {
		if item.ChannelRef != nil && item.ChannelRef.RepliesVia != nil {
			listed[item.ID] = item.ChannelRef.RepliesVia.ID
		}
	}
	if listed[fromBilling] != billing || listed[unknown] != support {
		t.Fatalf("the list says replies leave via %v, want %s for billing@ and the default %s for the other", listed, billing, support)
	}
	reply(t, api, admin, fromBilling, "From billing.")
	reply(t, api, admin, unknown, "From the default.")
	if got := deliveryChannel(t, pool, fromBilling); got != billing {
		t.Fatalf("the import of billing@ replied from %s, want %s", got, billing)
	}
	if got := deliveryChannel(t, pool, unknown); got != support {
		t.Fatalf("an import of another mailbox replied from %s, want the default %s", got, support)
	}
	if pinned := queryString(t, pool, `SELECT reply_channel_id::text FROM conversations WHERE id=$1`, fromBilling); pinned != billing {
		t.Fatalf("the first reply pinned %s, want %s", pinned, billing)
	}
}
