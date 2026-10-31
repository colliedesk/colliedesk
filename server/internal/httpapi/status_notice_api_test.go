package httpapi_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

const statusChat channels.Type = "status_chat"

// A status the team sets on a chat conversation tells its customer when the channel has words for it: a
// token that may not write to customers asks a person first (202, nothing changed), one that may sends
// it, and notify:false changes the status quietly for any token. Another workspace never reaches it.
func TestAStatusChangeThatTellsTheCustomerAsksADraftTokenFirst(t *testing.T) {
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
		response := request(t, api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "Claude " + scope, "scope": scope}, admin, nil)
		if response.Code != http.StatusCreated {
			t.Fatalf("token: %d %s", response.Code, response.Body.String())
		}
		var created struct {
			Token string `json:"token"`
		}
		decode(t, response, &created)
		return created.Token
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
	if response := request(t, api, http.MethodPatch, path, map[string]any{"status": "resolved"}, draft, nil); response.Code != http.StatusAccepted || statusOf() != "open" || notices() != "0" {
		t.Fatalf("draft token: %d %s status=%s notices=%s", response.Code, response.Body.String(), statusOf(), notices())
	}
	if response := request(t, api, http.MethodPatch, "/api/v1/conversations", map[string]any{"ids": []string{conversation}, "changes": map[string]any{"status": "resolved"}}, draft, nil); response.Code != http.StatusAccepted || statusOf() != "open" {
		t.Fatalf("draft token, bulk: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodPatch, path, map[string]any{"status": "pending", "notify": false}, draft, nil); response.Code != http.StatusOK || statusOf() != "pending" {
		t.Fatalf("draft token, quiet: %d %s", response.Code, response.Body.String())
	}
	if response := request(t, api, http.MethodPatch, path, map[string]any{"status": "resolved"}, send, nil); response.Code != http.StatusOK || statusOf() != "resolved" || notices() != "1" {
		t.Fatalf("send token: %d %s status=%s notices=%s", response.Code, response.Body.String(), statusOf(), notices())
	}
	if response := request(t, api, http.MethodPatch, path, map[string]any{"status": "open"}, other, nil); response.Code != http.StatusNotFound {
		t.Fatalf("another workspace: %d", response.Code)
	}
}
