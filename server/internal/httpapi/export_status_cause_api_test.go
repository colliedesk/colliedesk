package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// The export keeps who changed a status (a person, a token, a customer), whether the customer was told,
// the words a status request showed, when auto-close was turned on, the response targets, the automatic
// AI limits, a request for migration help, why a results baseline is unverified and a teammate's choice
// of reminder emails. Another workspace's are never in it.
func TestExportKeepsWhoChangedAStatusAndTheWorkspaceSettings(t *testing.T) {
	loop := channeltest.NewLoopback(statusChat, channels.Async)
	pool := testPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO channel_types(type,connectable) VALUES($1,true)`, string(statusChat)); err != nil {
		t.Fatal(err)
	}
	api, service, _, _ := testAPIOnPool(t, pool, func(config *httpapi.Config) { config.ExtraChannelTypes = []channels.Adapter{loop} })

	type desk struct {
		admin, channel, conversation, draft, send, request string
	}
	setUp := func(name, email, account string) desk {
		t.Helper()
		var d desk
		d.admin = createSession(t, api, service, name, email)
		response := request(t, api, http.MethodPost, "/api/v1/channels", map[string]any{"type": string(statusChat), "config": map[string]any{"account": account},
			"settings": map[string]any{"auto_close_hours": 24, "notices": map[string]any{"status_resolved": "{ref}: fixed by " + account + "."}}}, d.admin, nil)
		if response.Code != http.StatusCreated {
			t.Fatalf("channel: %d %s", response.Code, response.Body.String())
		}
		var channel struct {
			ID string `json:"id"`
		}
		decode(t, response, &channel)
		d.channel = channel.ID
		contact := create(t, api, d.admin, "/api/v1/contacts", map[string]any{"name": "Ada", "email": "ada@" + account + ".example.com"})
		d.conversation = create(t, api, d.admin, "/api/v1/conversations", map[string]any{"contact_id": contact, "subject": "Printer"})
		if _, err := pool.Exec(ctx, `WITH thread AS (
				INSERT INTO channel_threads(workspace_id,channel_id,kind,external_id) SELECT workspace_id,$2::uuid,'direct','user:ada' FROM conversations WHERE id=$1 RETURNING workspace_id,id),
			item AS (INSERT INTO channel_thread_items(workspace_id,channel_id,thread_id,direction,role,body,disposition,occurred_at)
				SELECT workspace_id,$2::uuid,id,'in','customer','it jams','conversation',now() FROM thread RETURNING workspace_id,id)
			INSERT INTO conversation_thread_items(workspace_id,conversation_id,item_id,reason) SELECT workspace_id,$1::uuid,id,'opened' FROM item`, d.conversation, d.channel); err != nil {
			t.Fatal(err)
		}
		token := func(scope string) string {
			t.Helper()
			response := request(t, api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": account + " " + scope, "scope": scope}, d.admin, nil)
			if response.Code != http.StatusCreated {
				t.Fatalf("token: %d %s", response.Code, response.Body.String())
			}
			var created struct {
				Token string `json:"token"`
			}
			decode(t, response, &created)
			return created.Token
		}
		draftToken, sendToken := token("draft"), token("send")
		d.draft = queryString(t, pool, `SELECT id::text FROM sessions WHERE label=$1`, account+" draft")
		d.send = queryString(t, pool, `SELECT id::text FROM sessions WHERE label=$1`, account+" send")
		path := "/api/v1/conversations/" + d.conversation
		// A draft token asks a person first: the request keeps what the customer would be told.
		response = request(t, api, http.MethodPatch, path, map[string]any{"status": "resolved"}, draftToken, nil)
		if response.Code != http.StatusAccepted {
			t.Fatalf("draft token: %d %s", response.Code, response.Body.String())
		}
		var filed struct {
			RequestID string `json:"request_id"`
		}
		decode(t, response, &filed)
		d.request = filed.RequestID
		// A send token changes it quietly, then a person reopens it.
		if response := request(t, api, http.MethodPatch, path, map[string]any{"status": "pending", "notify": false}, sendToken, nil); response.Code != http.StatusOK {
			t.Fatalf("send token, quiet: %d %s", response.Code, response.Body.String())
		}
		if response := request(t, api, http.MethodPatch, path, map[string]any{"status": "open"}, d.admin, nil); response.Code != http.StatusOK {
			t.Fatalf("reopen: %d %s", response.Code, response.Body.String())
		}
		return d
	}
	a := setUp("Cause Desk", "admin@cause.example.com", "cause-a")
	b := setUp("Cause Rival", "admin@cause-rival.example.com", "cause-b")

	settings := func(admin string, intakePerDay int) {
		t.Helper()
		if response := request(t, api, http.MethodPut, "/api/v1/ai/automatic-limits", map[string]any{"per_day": 300, "per_minute": 30, "intake_per_day": intakePerDay}, admin, nil); response.Code != http.StatusOK {
			t.Fatalf("automatic limits: %d %s", response.Code, response.Body.String())
		}
		if response := request(t, api, http.MethodPatch, "/api/v1/agents/me/sla-alerts", map[string]any{"email": false}, admin, nil); response.Code != http.StatusOK {
			t.Fatalf("sla alerts: %d %s", response.Code, response.Body.String())
		}
		if response := request(t, api, http.MethodPatch, "/api/v1/agents/me/results-email", map[string]any{"weekly": false}, admin, nil); response.Code != http.StatusOK {
			t.Fatalf("results email: %d %s", response.Code, response.Body.String())
		}
	}
	settings(a.admin, 123)
	settings(b.admin, 77)
	for _, row := range []struct{ conversation, timezone, freshdesk string }{
		{a.conversation, "Europe/Lisbon", "cause-a.freshdesk.com"},
		{b.conversation, "Pacific/Auckland", "cause-b.freshdesk.com"},
	} {
		if _, err := pool.Exec(ctx, `WITH w AS (SELECT workspace_id FROM conversations WHERE id=$1)
			INSERT INTO response_targets(workspace_id,enabled,timezone,hours,targets,warn_before_minutes,enabled_at)
			SELECT workspace_id,true,$2,'[{"day":1,"from":"09:00","to":"17:00"}]','{"urgent":{"first_response_minutes":60}}',15,'2026-10-01T08:00:00Z' FROM w`, row.conversation, row.timezone); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO workspace_migration_requests(workspace_id,requested_by,freshdesk_address,times,requested_at)
			SELECT c.workspace_id,a.id,$2,'Mornings, Lisbon time','2026-10-02T10:00:00Z' FROM conversations c JOIN agents a ON a.workspace_id=c.workspace_id WHERE c.id=$1`, row.conversation, row.freshdesk); err != nil {
			t.Fatal(err)
		}
		// Assigned by its admin, held over the Free quota, joined by a chat key and a reply token, and
		// imported from Freshdesk.
		if _, err := pool.Exec(ctx, `UPDATE conversations c SET assigned_at='2026-10-03T09:00:00Z',assigned_by_agent_id=a.id,held_at='2026-10-04T09:00:00Z'
			FROM agents a WHERE a.workspace_id=c.workspace_id AND c.id=$1`, row.conversation); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO conversation_thread_keys(workspace_id,conversation_id,channel_type,key)
			SELECT workspace_id,id,'email','reply-token:SECRET-'||$2 FROM conversations WHERE id=$1
			UNION ALL SELECT workspace_id,id,$3,'user:'||$2 FROM conversations WHERE id=$1`, row.conversation, row.freshdesk, string(statusChat)); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO import_items(workspace_id,source,entity,source_id,target_id,status,meta)
			SELECT workspace_id,'freshdesk','ticket','ticket-'||$2,id,'done','{"kept":true}' FROM conversations WHERE id=$1`, row.conversation, row.freshdesk); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO results_state(workspace_id,baseline_source,baseline_anchor_at,baseline_import_status,baseline_import_cause)
			SELECT workspace_id,'freshdesk','2026-09-01T00:00:00Z','UNVERIFIED','archive_refused' FROM conversations WHERE id=$1
			ON CONFLICT (workspace_id) DO UPDATE SET baseline_source=EXCLUDED.baseline_source,baseline_anchor_at=EXCLUDED.baseline_anchor_at,
				baseline_import_status=EXCLUDED.baseline_import_status,baseline_import_cause=EXCLUDED.baseline_import_cause`, row.conversation); err != nil {
			t.Fatal(err)
		}
	}

	response := request(t, api, http.MethodGet, "/api/v1/export", nil, a.admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d", response.Code)
	}
	entries := unzip(t, response.Body.Bytes())

	conversations := ndjsonLines(t, entries["conversations.ndjson"])
	if len(conversations) != 1 {
		t.Fatalf("conversations.ndjson has %d lines", len(conversations))
	}
	changes, _ := conversations[0]["status_changes"].([]any)
	if len(changes) != 2 {
		t.Fatalf("exported status_changes = %v", conversations[0]["status_changes"])
	}
	quiet, reopened := changes[0].(map[string]any), changes[1].(map[string]any)
	if quiet["to_status"] != "pending" || quiet["cause"] != "token" || quiet["actor_token_id"] != a.send || quiet["quiet"] != true {
		t.Fatalf("the token's quiet change exported as %v, want cause token, actor_token_id %s, quiet", quiet, a.send)
	}
	if reopened["to_status"] != "open" || reopened["cause"] != "agent" || reopened["actor_token_id"] != nil || reopened["quiet"] != false {
		t.Fatalf("the person's change exported as %v", reopened)
	}

	requests := ndjsonLines(t, entries["agent-requests.ndjson"])
	if len(requests) != 1 || requests[0]["id"] != a.request {
		t.Fatalf("agent-requests.ndjson = %v", requests)
	}
	notices, _ := requests[0]["notices"].([]any)
	if len(notices) != 1 || !strings.HasSuffix(notices[0].(map[string]any)["text"].(string), ": fixed by cause-a.") {
		t.Fatalf("exported notices = %v", requests[0]["notices"])
	}

	exported := ndjsonLines(t, entries["channels.ndjson"])
	if len(exported) != 1 || exported[0]["id"] != a.channel {
		t.Fatalf("channels.ndjson = %v", exported)
	}
	if since, _ := exported[0]["auto_close_since"].(string); !strings.HasSuffix(since, "Z") {
		t.Fatalf("auto_close_since = %v, want a UTC time", exported[0]["auto_close_since"])
	}

	targets := ndjsonLines(t, entries["response-targets.ndjson"])
	if len(targets) != 1 || targets[0]["enabled"] != true || targets[0]["timezone"] != "Europe/Lisbon" || targets[0]["warn_before_minutes"] != float64(15) ||
		targets[0]["enabled_at"] != "2026-10-01T08:00:00Z" || targets[0]["targets"] == nil || targets[0]["hours"] == nil {
		t.Fatalf("response-targets.ndjson = %v", targets)
	}
	limits := ndjsonLines(t, entries["ai-automatic-limits.ndjson"])
	if len(limits) != 1 || limits[0]["intake_per_day"] != float64(123) || limits[0]["per_day"] != float64(300) {
		t.Fatalf("ai-automatic-limits.ndjson = %v", limits)
	}
	migrations := ndjsonLines(t, entries["migration-requests.ndjson"])
	if len(migrations) != 1 || migrations[0]["freshdesk_address"] != "cause-a.freshdesk.com" || migrations[0]["times"] != "Mornings, Lisbon time" ||
		migrations[0]["requested_at"] != "2026-10-02T10:00:00Z" {
		t.Fatalf("migration-requests.ndjson = %v", migrations)
	}
	baselines := ndjsonLines(t, entries["results-baselines.ndjson"])
	if len(baselines) != 1 || baselines[0]["import_status"] != "UNVERIFIED" || baselines[0]["import_cause"] != "archive_refused" {
		t.Fatalf("results-baselines.ndjson = %v", baselines)
	}
	agents := ndjsonLines(t, entries["agents.ndjson"])
	if len(agents) != 1 || agents[0]["sla_alert_email"] != false || agents[0]["results_email"] != false {
		t.Fatalf("agents.ndjson = %v", agents)
	}
	if conversations[0]["assigned_at"] != "2026-10-03T09:00:00Z" || conversations[0]["assigned_by_agent_id"] != agents[0]["id"] || conversations[0]["held_at"] != "2026-10-04T09:00:00Z" {
		t.Fatalf("conversation assignment and hold = %v %v %v", conversations[0]["assigned_at"], conversations[0]["assigned_by_agent_id"], conversations[0]["held_at"])
	}
	keys := ndjsonLines(t, entries["conversation-thread-keys.ndjson"])
	if len(keys) != 1 || keys[0]["conversation_id"] != a.conversation || keys[0]["channel_type"] != string(statusChat) || keys[0]["key"] != "user:cause-a.freshdesk.com" {
		t.Fatalf("conversation-thread-keys.ndjson = %v", keys)
	}
	items := ndjsonLines(t, entries["import-items.ndjson"])
	if len(items) != 1 || items[0]["source_id"] != "ticket-cause-a.freshdesk.com" || items[0]["target_id"] != a.conversation || items[0]["status"] != "done" {
		t.Fatalf("import-items.ndjson = %v", items)
	}
	var manifest struct {
		Workspace struct {
			StatusHistorySince string `json:"status_history_since"`
		} `json:"workspace"`
	}
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil || !strings.HasSuffix(manifest.Workspace.StatusHistorySince, "Z") {
		t.Fatalf("manifest workspace = %s", entries["manifest.json"])
	}

	var all bytes.Buffer
	for name, content := range entries {
		all.WriteString(name + "\n")
		all.Write(content)
	}
	for _, foreign := range []string{b.channel, b.conversation, b.draft, b.send, b.request, "cause-b", "Pacific/Auckland", "SECRET-"} {
		if strings.Contains(all.String(), foreign) {
			t.Errorf("workspace A export contains %q (of workspace B, or a reply token)", foreign)
		}
	}
}

// A token's work is the token's in the export: a move that closes the conversation it empties, the undo
// that opens it again, and the note it writes all name the token, not only its owner.
func TestExportNamesTheTokenThatMovedMessagesAndWroteANote(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Token Moves", "admin@token-moves.example.com")
	secret := connectEmail(t, api, admin, "support@token-moves.example.com")
	response := request(t, api, http.MethodPost, "/api/v1/auth/tokens", map[string]any{"label": "Claude moves", "scope": "send"}, admin, nil)
	if response.Code != http.StatusCreated {
		t.Fatalf("token: %d %s", response.Code, response.Body.String())
	}
	var created struct {
		Token string `json:"token"`
	}
	decode(t, response, &created)
	tokenID := queryString(t, pool, `SELECT id::text FROM sessions WHERE label='Claude moves'`)
	first := inboundOK(t, api, secret, mail([]string{"From: kim@customer.example", "To: support@token-moves.example.com", "Subject: Printer", "Message-ID: <kim-1@customer.example>"}, "It jams"))
	second := inboundOK(t, api, secret, mail([]string{"From: lee@customer.example", "To: support@token-moves.example.com", "Subject: Printer too", "Message-ID: <lee-1@customer.example>"}, "Mine too"))
	printer, lee := *first.ConversationID, *second.ConversationID
	// The printer conversation was resolved by a person, telling the customer.
	if response := request(t, api, http.MethodPatch, "/api/v1/conversations/"+printer, map[string]any{"status": "resolved"}, admin, nil); response.Code != http.StatusOK {
		t.Fatalf("resolve: %d %s", response.Code, response.Body.String())
	}
	response = request(t, api, http.MethodPost, "/api/v1/conversations/"+lee+"/messages/move",
		map[string]any{"message_ids": []string{*second.MessageID}, "to": map[string]any{"conversation_id": printer}, "close_source": true}, created.Token, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("move: %d %s", response.Code, response.Body.String())
	}
	var moved moveResponse
	decode(t, response, &moved)
	if response := request(t, api, http.MethodPost, "/api/v1/message-moves/"+moved.Move.ID+"/undo", nil, created.Token, nil); response.Code != http.StatusOK {
		t.Fatalf("undo: %d %s", response.Code, response.Body.String())
	}
	_ = create(t, api, created.Token, "/api/v1/conversations/"+printer+"/messages", map[string]any{"kind": "note", "body": "Same printer as Lee."})

	response = request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("export status=%d", response.Code)
	}
	for _, conversation := range ndjsonLines(t, unzip(t, response.Body.Bytes())["conversations.ndjson"]) {
		if conversation["id"] == lee {
			changes, _ := conversation["status_changes"].([]any)
			if len(changes) != 2 {
				t.Fatalf("lee's status changes = %v, want closed by the move and opened by the undo", changes)
			}
			// A move tells the customer nothing: both changes are quiet.
			for _, change := range changes {
				if c := change.(map[string]any); c["cause"] != "token" || c["actor_token_id"] != tokenID || c["quiet"] != true {
					t.Fatalf("lee's change = %v, want cause token, actor_token_id %s, quiet", c, tokenID)
				}
			}
		}
		if conversation["id"] == printer {
			// Reopened by the move, quietly; the undo gives back the resolve as the person made it (told), so a
			// close by itself later tells the customer as it would have.
			changes, _ := conversation["status_changes"].([]any)
			var got []string
			for _, change := range changes {
				c := change.(map[string]any)
				got = append(got, fmt.Sprintf("%v:%v:%v", c["to_status"], c["cause"], c["quiet"]))
			}
			if strings.Join(got, ",") != "resolved:agent:false,open:token:true,resolved:token:false" {
				t.Fatalf("printer's status changes = %v", got)
			}
			found := false
			for _, message := range conversation["messages"].([]any) {
				if m := message.(map[string]any); m["kind"] == "note" {
					found = true
					if m["agent_token_id"] != tokenID {
						t.Fatalf("the token's note = %v, want agent_token_id %s", m, tokenID)
					}
				}
			}
			if !found {
				t.Fatal("the token's note is not exported")
			}
		}
	}
}
