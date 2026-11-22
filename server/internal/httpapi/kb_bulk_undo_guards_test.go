package httpapi_test

import (
	"net/http"
	"testing"
)

// A visibility change does not move an article's token, so undo compares the visibility itself: an
// article a teammate made visible to fewer people after the batch keeps their choice.
func TestUndoingABulkVisibilityChangeKeepsATeammatesNewerVisibility(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Visibility Undo", "admin@kb-bulk-visibility-undo.example.com")
	teammate := agentSession(t, api, admin, "agent@kb-bulk-visibility-undo.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	refunds := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"})
	shipping := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Shipping", "body_md": "Two days.", "status": "published"})
	changed := kbBulkAction(t, api, admin, map[string]any{"action": "visibility", "ids": []string{refunds.ID, shipping.ID}, "visibility": "help_chat"})
	if changed.Counts["done"] != 2 || changed.Operation == nil {
		t.Fatalf("bulk visibility: %v", changed.outcomes())
	}
	seen := kbVisibleArticleGet(t, api, teammate, refunds.ID)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+refunds.ID, map[string]any{"updated_at": seen.UpdatedAt, "visibility": "agents"}, teammate, http.StatusOK, nil)

	undone, _ := kbBulkUndo(t, api, admin, changed.Operation.ID, http.StatusOK)
	if got := undone.outcomes(); got[refunds.ID] != "changed_elsewhere" || got[shipping.ID] != "undone" {
		t.Fatalf("undo = %v, want the teammate's article left as changed_elsewhere and the other undone", got)
	}
	for id, want := range map[string]string{refunds.ID: "agents", shipping.ID: "everyone"} {
		if got := kbVisibleArticleGet(t, api, admin, id).Visibility; got != want {
			t.Errorf("%s visibility after undo = %s, want %s", id, got, want)
		}
	}
}

// An undo the caller may not do in full is refused before anything changes, and the batch stays
// undoable, so a teammate who may publish can still put it back.
func TestARefusedUndoLeavesTheBatchForSomeoneWhoMayDoIt(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Undo Refused", "admin@kb-bulk-undo-refused.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	refunds := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days."})
	shipping := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Shipping", "body_md": "Two days."})
	published := kbBulkAction(t, api, admin, map[string]any{"action": "publish", "ids": []string{refunds.ID, shipping.ID}})
	if published.Counts["done"] != 2 || published.Operation == nil {
		t.Fatalf("bulk publish: %v", published.outcomes())
	}
	editor := issueCustomToken(t, api, admin, "Docs bot", "kb_articles:read", "kb_articles:update", "kb_articles:delete")
	path := "/api/v1/kb/bulk-operations/" + published.Operation.ID + "/undo"
	refused(t, api, http.MethodPost, path, map[string]any{}, editor.Token, "kb_articles:publish")
	for _, id := range []string{refunds.ID, shipping.ID} {
		if got := kbGetArticle(t, api, admin, id).Status; got != "published" {
			t.Fatalf("%s after the refused undo is %s, want published", id, got)
		}
	}

	undone, _ := kbBulkUndo(t, api, admin, published.Operation.ID, http.StatusOK)
	if undone.Counts["undone"] != 2 {
		t.Fatalf("the admin's undo after a refused one = %v, want both undone", undone.outcomes())
	}
	for _, id := range []string{refunds.ID, shipping.ID} {
		if got := kbGetArticle(t, api, admin, id).Status; got != "draft" {
			t.Fatalf("%s after the admin's undo is %s, want draft", id, got)
		}
	}
}

// Undoing a trash of published articles puts them back on the help center, so it needs publish: a
// caller without it is refused and the articles stay in the trash.
func TestUndoingABulkTrashOfPublishedArticlesNeedsPublish(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Trash Scope", "admin@kb-bulk-trash-scope.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	live := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"})
	trashed := kbBulkAction(t, api, admin, map[string]any{"action": "trash", "ids": []string{live.ID}})
	editor := issueCustomToken(t, api, admin, "Docs bot", "kb_articles:read", "kb_articles:update", "kb_articles:delete")
	refused(t, api, http.MethodPost, "/api/v1/kb/bulk-operations/"+trashed.Operation.ID+"/undo", map[string]any{}, editor.Token, "kb_articles:publish")
	var trash struct {
		Items []kbArticleView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/trash", nil, admin, http.StatusOK, &trash)
	if len(trash.Items) != 1 || trash.Items[0].ID != live.ID {
		t.Fatalf("the article left the trash without publish: %d in the trash", len(trash.Items))
	}
}

// Raising an article's visibility back needs publish too, as raising it by hand does.
func TestUndoingABulkVisibilityLoweringNeedsPublishToRaiseItBack(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Raise Undo", "admin@kb-bulk-raise-undo.example.com")
	article := kbArticle(t, api, admin, map[string]any{"collection_id": kbCollection(t, api, admin, map[string]any{"name": "Docs"}).ID,
		"title": "Refunds", "body_md": "Within 14 days.", "status": "published"})
	lowered := kbBulkAction(t, api, admin, map[string]any{"action": "visibility", "ids": []string{article.ID}, "visibility": "agents"})
	editor := issueCustomToken(t, api, admin, "Docs bot", "kb_articles:read", "kb_articles:update")
	refused(t, api, http.MethodPost, "/api/v1/kb/bulk-operations/"+lowered.Operation.ID+"/undo", map[string]any{}, editor.Token, "kb_articles:publish")
	if got := kbVisibleArticleGet(t, api, admin, article.ID).Visibility; got != "agents" {
		t.Fatalf("visibility after the refused undo = %s, want agents", got)
	}
}
