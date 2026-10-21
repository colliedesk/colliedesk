package mcpserver_test

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var kbPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00synthetic")

func TestSendTokenWritesTheKnowledgeBaseWithHistoryTrashAndImages(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Tools Desk", "admin@kb-tools.example.com")
	session := e.connect(w.send, "")

	collection := e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Getting started"})
	collectionID := at(t, collection, "collection", "id").(string)
	child := e.ok(session, "colliedesk_kb_collection_create", map[string]any{"name": "Accounts", "parent_id": collectionID})
	childID := at(t, child, "collection", "id").(string)
	moved := e.ok(session, "colliedesk_kb_collection_update", map[string]any{"collection_id": childID, "updated_at": at(t, child, "collection", "updated_at"), "move_to_top": true})
	if at(t, moved, "collection", "parent_id") != nil {
		t.Fatalf("move to top = %v", moved)
	}

	upload := e.ok(session, "colliedesk_kb_image_upload", map[string]any{"data_base64": base64.StdEncoding.EncodeToString(kbPNG)})
	imageURL := at(t, upload, "image", "url").(string)
	imageID := at(t, upload, "image", "id").(string)
	shown := e.call(session, "colliedesk_kb_image_get", map[string]any{"image_id": imageID})
	if shown.IsError {
		t.Fatalf("image get failed: %s", text(shown))
	}
	var image *mcp.ImageContent
	for _, content := range shown.Content {
		if block, ok := content.(*mcp.ImageContent); ok {
			image = block
		}
	}
	if image == nil || image.MIMEType != "image/png" || !bytes.Equal(image.Data, kbPNG) {
		t.Fatalf("image content = %+v", shown.Content)
	}
	e.requireMatchesOutputSchema(session, "colliedesk_kb_image_get", structured(t, shown))

	created := e.ok(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Reset your password",
		"body": "Open **Settings**.\n\n![Screen](" + imageURL + ")", "status": "published"})
	articleID := at(t, created, "article", "id").(string)
	if at(t, created, "article", "number") != float64(1) || at(t, created, "article", "status") != "published" || at(t, created, "article", "last_change", "via_token") != "test send" {
		t.Fatalf("create = %v", created)
	}
	seen := at(t, created, "article", "updated_at")
	updated := e.ok(session, "colliedesk_kb_article_update", map[string]any{"article_id": articleID, "updated_at": seen, "body": "Open **Settings**, then *Password*.", "status": "draft"})
	if at(t, updated, "article", "status") != "draft" || at(t, updated, "article", "team_text", "body") != "Open **Settings**, then *Password*." {
		t.Fatalf("update = %v", updated)
	}
	if message := e.fails(session, "colliedesk_kb_article_update", map[string]any{"article_id": articleID, "updated_at": seen, "title": "Overwrite"}); !strings.Contains(message, "changed by someone else") {
		t.Errorf("stale update: %q", message)
	}
	found := e.ok(session, "colliedesk_kb_articles_search", map[string]any{"query": "password"})["articles"].([]any)
	if len(found) != 1 || at(t, found[0], "team_text", "title") != "Reset your password" {
		t.Fatalf("search = %v", found)
	}
	restored := e.ok(session, "colliedesk_kb_article_version_restore", map[string]any{"article_id": articleID, "version": 1, "updated_at": at(t, updated, "article", "updated_at")})
	if !strings.HasPrefix(at(t, restored, "article", "team_text", "body").(string), "Open **Settings**.\n\n![Screen]") {
		t.Fatalf("restore = %v", restored)
	}
	deleted := e.ok(session, "colliedesk_kb_article_delete", map[string]any{"article_id": articleID, "updated_at": at(t, restored, "article", "updated_at")})
	if !strings.Contains(deleted["next"].(string), "colliedesk_kb_article_undelete") {
		t.Fatalf("delete = %v", deleted)
	}
	trash := e.ok(session, "colliedesk_kb_articles_trash", nil)["articles"].([]any)
	if len(trash) != 1 || at(t, trash[0], "purges_at") == nil {
		t.Fatalf("trash = %v", trash)
	}
	if got := e.ok(session, "colliedesk_kb_article_get", map[string]any{"article_id": articleID}); at(t, got, "article", "in_trash") != true {
		t.Fatalf("get of a trashed article = %v", got)
	}
	e.ok(session, "colliedesk_kb_article_undelete", map[string]any{"article_id": articleID, "updated_at": at(t, trash[0], "updated_at")})
	history := e.ok(session, "colliedesk_kb_article_history", map[string]any{"article_id": articleID})["versions"].([]any)
	var kinds []string
	for _, version := range history {
		kinds = append(kinds, at(t, version, "kind").(string))
	}
	if strings.Join(kinds, ",") != "undelete,delete,restore,edit,create" {
		t.Fatalf("history = %v", kinds)
	}
	whole := e.ok(session, "colliedesk_kb_article_history", map[string]any{"article_id": articleID, "version": 2})["versions"].([]any)
	if len(whole) != 1 || at(t, whole[0], "status") != "draft" {
		t.Fatalf("version 2 = %v", whole)
	}

	e.ok(session, "colliedesk_kb_collection_delete", map[string]any{"collection_id": childID, "updated_at": at(t, moved, "collection", "updated_at")})
	collections := e.ok(session, "colliedesk_kb_collections_trash", nil)["collections"].([]any)
	if len(collections) != 1 || at(t, collections[0], "id") != childID {
		t.Fatalf("collection trash = %v", collections)
	}
	e.ok(session, "colliedesk_kb_collection_undelete", map[string]any{"collection_id": childID, "updated_at": at(t, collections[0], "updated_at")})
	if listed := e.ok(session, "colliedesk_kb_collections_list", nil)["collections"].([]any); len(listed) != 2 {
		t.Fatalf("collections = %v", listed)
	}
	if center := e.ok(session, "colliedesk_help_center_get", nil); at(t, center, "help_center", "enabled") != false {
		t.Fatalf("help center = %v", center)
	}
}

func TestLowerTiersProposeKnowledgeBaseChangesAndNeverUploadImages(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Proposal Desk", "admin@kb-proposal.example.com")
	collectionID := e.created("/api/v1/kb/collections", map[string]any{"name": "Docs"}, w.admin)
	read := e.connect(w.read, "")

	proposed := e.ok(read, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Proposed", "body": "Text"})
	if proposed["agent_request"] == nil {
		t.Fatalf("read token create = %v, want an agent request", proposed)
	}
	if found := e.ok(read, "colliedesk_kb_articles_search", nil)["articles"].([]any); len(found) != 0 {
		t.Fatalf("a proposal wrote %v", found)
	}
	if message := e.fails(read, "colliedesk_kb_image_upload", map[string]any{"data_base64": base64.StdEncoding.EncodeToString(kbPNG)}); !strings.Contains(strings.ToLower(message), "file") {
		t.Errorf("read token upload: %q", message)
	}
	send := e.connect(w.send, "")
	turnedOn := e.ok(send, "colliedesk_help_center_update", map[string]any{"enabled": true, "title": "Help", "locale": "en"})
	if turnedOn["agent_request"] == nil {
		t.Fatalf("send token help center = %v, want an agent request", turnedOn)
	}
	admin := e.connect(w.adminTier, "")
	if center := e.ok(admin, "colliedesk_help_center_update", map[string]any{"enabled": true, "title": "Help", "locale": "en"}); at(t, center, "help_center", "enabled") != true {
		t.Fatalf("admin token help center = %v", center)
	}
}

func TestKnowledgeBaseToolsStayInTheirWorkspace(t *testing.T) {
	e := newEnv(t)
	alpha := e.newWorkspace("KB Alpha Tools", "admin@kb-alpha-tools.example.com")
	beta := e.newWorkspace("KB Beta Tools", "admin@kb-beta-tools.example.com")
	collectionID := e.created("/api/v1/kb/collections", map[string]any{"name": "Alpha"}, alpha.admin)
	articleID := e.created("/api/v1/kb/articles", map[string]any{"collection_id": collectionID, "title": "Alpha secret", "body_md": "x"}, alpha.admin)
	session := e.connect(beta.send, "")
	if message := e.fails(session, "colliedesk_kb_article_get", map[string]any{"article_id": articleID}); !strings.Contains(message, "No knowledge base article") {
		t.Errorf("beta reads alpha's article: %q", message)
	}
	if message := e.fails(session, "colliedesk_kb_collection_delete", map[string]any{"collection_id": collectionID, "updated_at": "2026-10-20T00:00:00Z"}); !strings.Contains(message, "No knowledge base collection") {
		t.Errorf("beta deletes alpha's collection: %q", message)
	}
	if found := e.ok(session, "colliedesk_kb_articles_search", map[string]any{"query": "secret"})["articles"].([]any); len(found) != 0 {
		t.Errorf("beta search found %v", found)
	}
	if message := e.fails(session, "colliedesk_kb_article_create", map[string]any{"collection_id": collectionID, "title": "Smuggled", "body": "x"}); message == "" {
		t.Error("beta wrote into alpha's collection")
	}
}
