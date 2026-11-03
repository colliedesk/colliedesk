package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

const statusChat channels.Type = "status_chat"

// A status the team sets on a chat conversation tells its customer when the channel has words for it: a
// token that may not write to customers asks a person first (202, nothing changed), one that may sends
// it, and notify:false changes the status quietly for any token. Another workspace never reaches it.
func TestAStatusChangeThatTellsTheCustomerNeedsMessagesSend(t *testing.T) {
	loop := channeltest.NewLoopback(statusChat, channels.Async)
	pool := testPool(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO channel_types(type,connectable) VALUES($1,true)`, string(statusChat)); err != nil {
		t.Fatal(err)
	}
	api, service, _, _ := testAPIOnPool(t, pool, func(config *httpapi.Config) { config.ExtraChannelTypes = []channels.Adapter{loop} })
	admin := createSession(t, api, service, "Status Desk", "admin@status.example.com")
	other := createSession(t, api, service, "Status Other", "admin@status-other.example.com")
	response := request(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": string(statusChat), "config": map[string]any{"account": "status-a"},
		"settings": map[string]any{"notices": map[string]any{"status_resolved": "{ref}: fixed."}}}, admin, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("channel: %d %s", response.Code, response.Body.String())
	}
	var channel struct {
		ID string `json:"id"`
	}
	decode(t, response, &channel)
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@status.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer"})
	// The conversation is in a chat of the channel (as one the channel opened is).
	if _, err := pool.Exec(context.Background(), `WITH thread AS (
			INSERT INTO channel_threads(workspace_id,channel_id,kind,external_id) SELECT workspace_id,$2::uuid,'direct','user:ada' FROM conversations WHERE id=$1 RETURNING workspace_id,id),
		item AS (INSERT INTO channel_thread_items(workspace_id,channel_id,thread_id,direction,role,body,disposition,occurred_at)
			SELECT workspace_id,$2::uuid,id,'in','customer','it jams','conversation',now() FROM thread RETURNING workspace_id,id)
		INSERT INTO conversation_thread_items(workspace_id,conversation_id,item_id,reason) SELECT workspace_id,$1::uuid,id,'opened' FROM item`, conversation, channel.ID); err != nil {
		t.Fatal(err)
	}
	token := func(scope string) string {
		t.Helper()
		return issueToken(t, api, admin, "Claude "+scope, scope).Token
	}
	draft, send := token("draft"), token("send")
	notices := func() string {
		t.Helper()
		return queryString(t, pool, `SELECT count(*)::text FROM messages WHERE conversation_id=$1 AND notice_kind='status_resolved'`, conversation)
	}
	statusOf := func() string {
		t.Helper()
		return queryString(t, pool, `SELECT status FROM conversations WHERE id=$1`, conversation)
	}
	path := "/api/v1/conversations/" + conversation
	ref := queryString(t, pool, `SELECT display_ref FROM conversations WHERE id=$1`, conversation)
	lastChange := func() string {
		t.Helper()
		response := request(t, api, http.MethodGet, path, nil, admin, nil)
		var item struct {
			LastStatusChange *struct {
				To, By string
				Name   *string
			} `json:"last_status_change"`
		}
		decode(t, response, &item)
		if item.LastStatusChange == nil || item.LastStatusChange.Name == nil {
			return fmt.Sprintf("%+v", item.LastStatusChange)
		}
		return item.LastStatusChange.To + " by " + item.LastStatusChange.By + ": " + *item.LastStatusChange.Name
	}
	// A draft token may not write to customers: a status that tells one is refused, and says how to change it quietly.
	response = request(t, api, http.MethodPatch, path, map[string]any{"status": "resolved"}, draft, nil)
	if missing, names, _ := permissionMissing(t, response); !missing || !slices.Equal(names, []string{"messages:send"}) || !strings.Contains(response.Body.String(), "notify: false") ||
		statusOf() != "open" || notices() != "0" {
		t.Fatalf("draft token: %d %s status=%s notices=%s", response.Code, response.Body.String(), statusOf(), notices())
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/conversations", map[string]any{"ids": []string{conversation}, "changes": map[string]any{"status": "resolved"}}, draft, nil); response.Code != http.StatusForbidden || statusOf() != "open" {
		t.Fatalf("draft token, bulk: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodPatch, path, map[string]any{"status": "pending", "notify": false}, draft, nil); response.Code != http.StatusOK || statusOf() != "pending" {
		t.Fatalf("draft token, quiet: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodPatch, path, map[string]any{"status": "resolved"}, send, nil); response.Code != http.StatusOK || statusOf() != "resolved" || notices() != "1" {
		t.Fatalf("send token: %d %s status=%s notices=%s", response.Code, response.Body.String(), statusOf(), notices())
	}
	if got := lastChange(); got != "resolved by token: Claude send" {
		t.Fatalf("token change = %s", got)
	}
	// The conversation came by no chat; its chat is told through the chat's channel, never by email.
	if got := queryString(t, pool, `SELECT COALESCE(o.channel_id::text,'none') FROM messages m LEFT JOIN channel_outbox o ON o.message_id=m.id WHERE m.conversation_id=$1 AND m.notice_kind='status_resolved'`, conversation); got != channel.ID {
		t.Fatalf("the notice went through %s", got)
	}
	// A status the conversation has already tells nobody, so the draft token may set it.
	if response := request(t, api, http.MethodPatch, path, map[string]any{"status": "resolved"}, draft, nil); response.Code != http.StatusOK || notices() != "1" {
		t.Fatalf("draft token, a status that tells nobody: %d %s notices=%s", response.Code, response.Body.String(), notices())
	}
	if response := request(t, api, http.MethodPatch, path, map[string]any{"status": "open"}, other, nil); response.Code != http.StatusNotFound {
		t.Fatalf("another workspace: %d", response.Code)
	}
	_ = ref
}

// A status notice the chat refused is retried in that chat, through its channel, never by the
// conversation's email, and the conversation's replies stay where they were.
func TestARetriedStatusNoticeStaysInItsChat(t *testing.T) {
	loop := channeltest.NewLoopback(statusChat, channels.Async)
	pool := testPool(t)
	if _, err := pool.Exec(context.Background(), `INSERT INTO channel_types(type,connectable) VALUES($1,true)`, string(statusChat)); err != nil {
		t.Fatal(err)
	}
	api, service, _, _ := testAPIOnPool(t, pool, func(config *httpapi.Config) { config.ExtraChannelTypes = []channels.Adapter{loop} })
	admin := createSession(t, api, service, "Retry Desk", "admin@retry.example.com")
	response := request(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": string(statusChat), "config": map[string]any{"account": "retry-a"},
		"settings": map[string]any{"notices": map[string]any{"status_resolved": "{ref}: fixed."}}}, admin, nil)
	var channel struct{ ID string }
	decode(t, response, &channel)
	if mail := request(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": "email", "config": map[string]any{"support_address": "help@retry.example.com"}}, admin, nil); mail.Code != http.StatusCreated {
		t.Fatalf("email channel: %d %s", mail.Code, mail.Body.String())
	}
	contact := create(t, api, admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@retry.example.com"})
	conversation := create(t, api, admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer"})
	if _, err := pool.Exec(context.Background(), `WITH thread AS (
			INSERT INTO channel_threads(workspace_id,channel_id,kind,external_id) SELECT workspace_id,$2::uuid,'direct','user:ada' FROM conversations WHERE id=$1 RETURNING workspace_id,id),
		item AS (INSERT INTO channel_thread_items(workspace_id,channel_id,thread_id,direction,role,body,disposition,occurred_at)
			SELECT workspace_id,$2::uuid,id,'in','customer','it jams','conversation',now() FROM thread RETURNING workspace_id,id)
		INSERT INTO conversation_thread_items(workspace_id,conversation_id,item_id,reason) SELECT workspace_id,$1::uuid,id,'opened' FROM item`, conversation, channel.ID); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/conversations/" + conversation
	if response := request(t, api, http.MethodPatch, path, map[string]any{"status": "resolved"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("resolve: %d %s", response.Code, response.Body.String())
	}
	message := queryString(t, pool, `SELECT id::text FROM messages WHERE conversation_id=$1 AND notice_kind='status_resolved'`, conversation)
	pinned := queryString(t, pool, `SELECT COALESCE(reply_channel_id::text,'none') FROM conversations WHERE id=$1`, conversation)
	// The chat refuses it, as Zalo can: a failure a person may retry.
	if _, err := pool.Exec(context.Background(), `UPDATE channel_outbox SET status='failed',last_error='refused',last_error_code='refused',final=false WHERE message_id=$1`, message); err != nil {
		t.Fatal(err)
	}
	if response := request(t, api, http.MethodPost, path+"/messages/"+message+"/retry", nil, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", response.Code, response.Body.String())
	}
	if got := queryString(t, pool, `SELECT o.channel_id::text||'/'||o.status||'/'||m.channel_id::text FROM channel_outbox o JOIN messages m ON m.id=o.message_id WHERE o.message_id=$1`, message); got != channel.ID+"/queued/"+channel.ID {
		t.Fatalf("after retry = %s, want the chat's channel %s", got, channel.ID)
	}
	if got := queryString(t, pool, `SELECT COALESCE(reply_channel_id::text,'none') FROM conversations WHERE id=$1`, conversation); got != pinned {
		t.Fatalf("the retry moved the conversation's replies: %s, was %s", got, pinned)
	}
}
