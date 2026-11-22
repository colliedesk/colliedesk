package mcpserver_test

import (
	"slices"
	"strings"
	"testing"
)

func TestAnAgentTagsEveryArticleOfACollectionInOneCallAndUndoesIt(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Bulk Desk", "admin@kb-bulk.example.com")
	send := e.connect(w.send, "")
	billing := at(t, e.ok(send, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"}), "collection", "id").(string)
	other := at(t, e.ok(send, "colliedesk_kb_collection_create", map[string]any{"name": "Shipping"}), "collection", "id").(string)
	var ids []string
	for _, title := range []string{"Refund rules", "Invoices", "Card declined"} {
		created := e.ok(send, "colliedesk_kb_article_create", map[string]any{"collection_id": billing, "title": title, "body": "Text."})
		ids = append(ids, at(t, created, "article", "id").(string))
	}
	outside := at(t, e.ok(send, "colliedesk_kb_article_create", map[string]any{"collection_id": other, "title": "Couriers", "body": "Text."}), "article", "id").(string)

	tagged := e.ok(send, "colliedesk_kb_articles_bulk", map[string]any{"action": "tags_add", "tags": []string{"refunds"}, "filter": map[string]any{"collection_id": billing}})
	e.requireMatchesOutputSchema(send, "colliedesk_kb_articles_bulk", tagged)
	if done := at(t, tagged, "counts", "done"); done != float64(3) {
		t.Fatalf("bulk tag of Billing = %v, want 3 done", tagged)
	}
	var titles []string
	for _, row := range tagged["results"].([]any) {
		titles = append(titles, at(t, row, "team_text", "title").(string))
	}
	slices.Sort(titles)
	if !slices.Equal(titles, []string{"Card declined", "Invoices", "Refund rules"}) {
		t.Fatalf("bulk results name %v, want the three Billing titles", titles)
	}
	for _, id := range ids {
		if tags := at(t, e.ok(send, "colliedesk_kb_article_get", map[string]any{"article_id": id}), "article", "tags"); !slices.Equal(anyStrings(t, tags), []string{"refunds"}) {
			t.Fatalf("article %s tags = %v after bulk tag, want [refunds]", id, tags)
		}
	}
	if tags := at(t, e.ok(send, "colliedesk_kb_article_get", map[string]any{"article_id": outside}), "article", "tags"); len(anyStrings(t, tags)) != 0 {
		t.Fatalf("article outside the filter got tags %v", tags)
	}

	undone := e.ok(send, "colliedesk_kb_articles_bulk_undo", map[string]any{"operation_id": at(t, tagged, "operation", "id")})
	e.requireMatchesOutputSchema(send, "colliedesk_kb_articles_bulk_undo", undone)
	if count := at(t, undone, "counts", "undone"); count != float64(3) {
		t.Fatalf("undo = %v, want 3 undone", undone)
	}
	for _, id := range ids {
		if tags := at(t, e.ok(send, "colliedesk_kb_article_get", map[string]any{"article_id": id}), "article", "tags"); len(anyStrings(t, tags)) != 0 {
			t.Fatalf("article %s tags = %v after undo, want none", id, tags)
		}
	}
	again := e.fails(send, "colliedesk_kb_articles_bulk_undo", map[string]any{"operation_id": at(t, tagged, "operation", "id")})
	if !strings.Contains(again, "already") {
		t.Fatalf("second undo = %q, want it refused as already undone", again)
	}
}

func TestBulkToolsRefuseATokenWithoutThePermissionTheActionNeeds(t *testing.T) {
	e := newEnv(t)
	w := e.newWorkspace("KB Bulk Scope Desk", "admin@kb-bulk-scope.example.com")
	send := e.connect(w.send, "")
	collection := at(t, e.ok(send, "colliedesk_kb_collection_create", map[string]any{"name": "Billing"}), "collection", "id").(string)
	id := at(t, e.ok(send, "colliedesk_kb_article_create", map[string]any{"collection_id": collection, "title": "Refund rules", "body": "Text."}), "article", "id").(string)

	reader := e.connect(e.token(w.admin, "reader", "", []string{"kb_articles:read"}), "")
	e.refused(reader, "colliedesk_kb_articles_bulk", map[string]any{"action": "tags_add", "tags": []string{"x"}, "ids": []string{id}})
	e.refused(reader, "colliedesk_kb_articles_bulk_undo", map[string]any{"operation_id": "00000000-0000-4000-8000-000000000000"})

	editor := e.connect(e.token(w.admin, "editor", "", []string{"kb_articles:read", "kb_articles:update"}), "")
	message := e.fails(editor, "colliedesk_kb_articles_bulk", map[string]any{"action": "publish", "ids": []string{id}})
	if !strings.Contains(message, "kb_articles:publish") {
		t.Fatalf("bulk publish without publish = %q, want it to name kb_articles:publish", message)
	}
	if status := at(t, e.ok(send, "colliedesk_kb_article_get", map[string]any{"article_id": id}), "article", "status"); status != "draft" {
		t.Fatalf("article status = %v after a refused bulk publish, want draft", status)
	}
}
