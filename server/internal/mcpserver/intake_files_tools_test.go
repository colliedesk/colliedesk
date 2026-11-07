package mcpserver_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/attachments"
	"github.com/colliedesk/colliedesk/server/internal/channels"
	"github.com/colliedesk/colliedesk/server/internal/channels/channeltest"
	"github.com/colliedesk/colliedesk/server/internal/httpapi"
)

// An agent sees the picture a customer sent on an intake card before deciding it, as a person does: the card
// names its files and colliedesk_attachment_get reads one by card, item and index, only in its own workspace.
func TestAnAgentReadsAPictureOnAnIntakeCard(t *testing.T) {
	loop := channeltest.NewLoopback("mcpintakefiles", channels.Async)
	loop.Defaults.Scopes = map[string]channels.Scope{"group": {Gather: &channels.Gather{QuietSeconds: 300, MaxSeconds: 1200}}}
	loop.IntakeScopes = []string{"group"}
	var storage attachments.Storage
	e := newEnvWith(t, func(config *httpapi.Config) {
		config.ExtraChannelTypes = []channels.Adapter{loop}
		storage = config.Attachments
	})
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `INSERT INTO channel_types(type,connectable) VALUES('mcpintakefiles',true)`); err != nil {
		t.Fatal(err)
	}
	w := e.newWorkspace("Intake Files Desk", "admin@intake-files.example.com")
	other := e.newWorkspace("Other Files Desk", "admin@other-files.example.com")
	status, created := e.rest(http.MethodPost, "/api/v1/channels", map[string]any{"type": "mcpintakefiles", "config": map[string]any{"account": "intake-files"}}, w.admin)
	if status != http.StatusCreated {
		t.Fatalf("create channel: %d %v", status, created)
	}
	var picture bytes.Buffer
	if err := png.Encode(&picture, image.NewNRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	object, err := storage.Put(ctx, w.id, bytes.NewReader(picture.Bytes()), int64(picture.Len()))
	if err != nil {
		t.Fatal(err)
	}
	var card, item string
	if err := e.pool.QueryRow(ctx, `WITH th AS (INSERT INTO channel_threads(workspace_id,channel_id,kind,external_id,opens_conversations)
			SELECT workspace_id,id,'group','group:g1',true FROM channels WHERE id=$1 RETURNING workspace_id,channel_id,id),
		g AS (INSERT INTO intake_gatherings(workspace_id,channel_id,scope,lane_key,thread_id,state,review_reason,rules_action,rules_rule,opened_at,closed_at)
			SELECT workspace_id,channel_id,'group','thread:'||id||':u1',id,'review','no_command','review','review_hold',now(),now() FROM th RETURNING workspace_id,channel_id,id,thread_id),
		e AS (INSERT INTO channel_inbound_events(workspace_id,channel_id,external_event_id,state,raw,sender_key,hold_reason,gathering_id,received_at,attempts)
			SELECT workspace_id,channel_id,'p-1','held','{"id":"p-1","from":"u1","group":"g1","text":""}','loopback_user:u1','intake',id,now(),1 FROM g RETURNING id,workspace_id),
		f AS (INSERT INTO channel_inbound_files(workspace_id,event_id,idx,name,content_type,status,storage_key,sha256,size)
			SELECT workspace_id,id,0,'image-1','image/png','stored',$2,$3,$4 FROM e RETURNING event_id)
		INSERT INTO channel_thread_items(workspace_id,channel_id,thread_id,direction,role,sender_kind,sender_value,sender_name,body,inbound_event_id,gathering_id,disposition,occurred_at)
			SELECT g.workspace_id,g.channel_id,g.thread_id,'in','customer','loopback_user','u1','Toan','[picture]',e.id,g.id,'pending_review',now()
			FROM g, e, f RETURNING gathering_id::text,id::text`, created["id"], object.Key, object.SHA256, object.Size).Scan(&card, &item); err != nil {
		t.Fatal(err)
	}
	reader := e.connect(w.read, "")
	listed := e.ok(reader, "colliedesk_intake_list", map[string]any{})
	file := at(t, listed, "items", 0, "files", 0)
	if at(t, file, "item_id") != item || at(t, file, "file_index") != float64(0) || at(t, file, "status") != "stored" ||
		at(t, file, "untrusted_customer_content", "name") != "image-1" {
		t.Fatalf("card file = %v", file)
	}
	args := map[string]any{"gathering_id": card, "item_id": item, "file_index": 0}
	result := e.call(reader, "colliedesk_attachment_get", args)
	if result.IsError {
		t.Fatalf("read the card's picture: %s", text(result))
	}
	out := structured(t, result)
	e.requireMatchesOutputSchema(reader, "colliedesk_attachment_get", out)
	if shown := imageOf(result); shown == nil || shown.MIMEType != "image/png" || !bytes.Equal(shown.Data, picture.Bytes()) || at(t, out, "is_image") != true {
		t.Fatalf("picture = %v %v", result.Content, out)
	}
	if message := e.fails(e.connect(other.read, ""), "colliedesk_attachment_get", args); !strings.Contains(message, "No stored file") {
		t.Fatalf("another workspace reads the card's picture: %s", message)
	}
	if message := e.fails(reader, "colliedesk_attachment_get", map[string]any{"gathering_id": card, "item_id": item}); !strings.Contains(message, "needs gathering_id, item_id and file_index") {
		t.Fatalf("half a card file: %s", message)
	}
	if message := e.fails(reader, "colliedesk_attachment_get", map[string]any{}); !strings.Contains(message, "Give attachment_id") {
		t.Fatalf("nothing named: %s", message)
	}
}
