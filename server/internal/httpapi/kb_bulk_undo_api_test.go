package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
)

func kbBulkUndo(t *testing.T, api http.Handler, token, operation string, want int) (kbBatch, string) {
	t.Helper()
	var answer kbBatch
	response := kbCall(t, api, http.MethodPost, "/api/v1/kb/bulk-operations/"+operation+"/undo", map[string]any{}, token, want, &answer)
	if want != http.StatusOK {
		var refusal struct {
			Error struct{ Code, Message string } `json:"error"`
		}
		decode(t, response, &refusal)
		return answer, refusal.Error.Code + ": " + refusal.Error.Message
	}
	return answer, ""
}

// kbCollectionTitles is the titles a collection lists, in its order.
func kbCollectionTitles(t *testing.T, api http.Handler, token, collection string) []string {
	t.Helper()
	var page struct {
		Items []kbArticleView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles?limit=200&collection_id="+collection, nil, token, http.StatusOK, &page)
	titles := []string{}
	for _, item := range page.Items {
		titles = append(titles, item.Title)
	}
	return titles
}

// Undo puts back what the batch changed, each article in its own history, except an article someone
// changed since: that one is left as it is and listed, so undo never overwrites newer work. A batch is
// undone once, and another workspace cannot tell its id from one that does not exist.
func TestUndoingABulkMoveLeavesAnArticleChangedSinceAndListsIt(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Undo", "admin@kb-bulk-undo.example.com")
	from := kbCollection(t, api, admin, map[string]any{"name": "Billing"})
	to := kbCollection(t, api, admin, map[string]any{"name": "Archive"})
	refunds := kbArticle(t, api, admin, map[string]any{"collection_id": from.ID, "title": "Refunds", "body_md": "Within 14 days."})
	shipping := kbArticle(t, api, admin, map[string]any{"collection_id": from.ID, "title": "Shipping", "body_md": "Two days."})
	returns := kbArticle(t, api, admin, map[string]any{"collection_id": from.ID, "title": "Returns", "body_md": "By post."})
	ids := []string{refunds.ID, shipping.ID, returns.ID}
	before := map[string]string{}
	for _, id := range ids {
		before[id] = kbVersionKinds(t, api, admin, id)
	}

	moved := kbBulkAction(t, api, admin, map[string]any{"action": "move", "ids": ids, "collection_id": to.ID})
	if moved.Counts["done"] != 3 || moved.Operation == nil || moved.Operation.UndoUntil == "" {
		t.Fatalf("bulk move: counts=%v operation=%v", moved.Counts, moved.Operation)
	}
	for _, id := range ids {
		if got := kbVersionKinds(t, api, admin, id); strings.Count(got, ",") != strings.Count(before[id], ",")+1 {
			t.Fatalf("%s history after the move = %q, was %q: want one entry of its own", id, got, before[id])
		}
	}
	edited := kbGetArticle(t, api, admin, shipping.ID)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+shipping.ID, map[string]any{"title": "Shipping times", "updated_at": kbSeen(edited)}, admin, http.StatusOK, nil)

	other := createSession(t, api, service, "KB Bulk Undo Other", "admin@kb-bulk-undo-other.example.com")
	_, theirs := kbBulkUndo(t, api, other, moved.Operation.ID, http.StatusNotFound)
	_, nowhere := kbBulkUndo(t, api, other, "00000000-0000-4000-8000-000000000679", http.StatusNotFound)
	if theirs != nowhere {
		t.Fatalf("another workspace's batch answers %q, a missing one %q", theirs, nowhere)
	}

	undone, _ := kbBulkUndo(t, api, admin, moved.Operation.ID, http.StatusOK)
	want := map[string]string{refunds.ID: "undone", returns.ID: "undone", shipping.ID: "changed_elsewhere"}
	if got := undone.outcomes(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("undo = %v, want %v", got, want)
	}
	if undone.Counts["undone"] != 2 || undone.Counts["not_undone"] != 1 {
		t.Fatalf("undo counts = %v, want 2 undone and 1 not undone", undone.Counts)
	}
	for id, collection := range map[string]string{refunds.ID: from.ID, returns.ID: from.ID, shipping.ID: to.ID} {
		if got := kbGetArticle(t, api, admin, id).CollectionID; got != collection {
			t.Fatalf("%s is in %s after undo, want %s", id, got, collection)
		}
	}
	if got := kbGetArticle(t, api, admin, shipping.ID).Title; got != "Shipping times" {
		t.Fatalf("the edited article's title became %q", got)
	}
	if _, code := kbBulkUndo(t, api, admin, moved.Operation.ID, http.StatusConflict); !strings.HasPrefix(code, "already_undone") {
		t.Fatalf("a second undo: %s", code)
	}
}

// Undo works for ten minutes after the batch; later it is refused and nothing changes.
func TestABulkActionIsUndoneOnlyWithinTenMinutes(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "KB Bulk Expiry", "admin@kb-bulk-expiry.example.com")
	article := kbArticle(t, api, admin, map[string]any{"collection_id": kbCollection(t, api, admin, map[string]any{"name": "Docs"}).ID, "title": "Refunds", "body_md": "Within 14 days."})
	tagged := kbBulkAction(t, api, admin, map[string]any{"action": "tags_add", "ids": []string{article.ID}, "tags": []string{"billing"}})
	if _, err := pool.Exec(context.Background(), `UPDATE kb_bulk_operations SET created_at=created_at-interval '11 minutes' WHERE id=$1`, tagged.Operation.ID); err != nil {
		t.Fatal(err)
	}
	if _, code := kbBulkUndo(t, api, admin, tagged.Operation.ID, http.StatusConflict); !strings.HasPrefix(code, "undo_expired") {
		t.Fatalf("undo after 11 minutes: %s", code)
	}
	if got := kbArticleIDs(t, api, admin, "tag=billing"); len(got) != 1 {
		t.Fatalf("an expired undo removed the tag: %v", got)
	}
}

// Fifty articles ordered by hand are moved out in one batch; undo puts every one back in the same order.
func TestUndoingABulkMoveOfFiftyArticlesPutsThemBackInTheirOrder(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Fifty", "admin@kb-bulk-fifty.example.com")
	from := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	to := kbCollection(t, api, admin, map[string]any{"name": "Old guides"})
	ids, titles := []string{}, []string{}
	for i := range 50 {
		title := fmt.Sprintf("Guide %02d", i)
		ids = append(ids, kbArticle(t, api, admin, map[string]any{"collection_id": from.ID, "title": title, "body_md": "Step."}).ID)
		titles = append(titles, title)
	}
	slices.Reverse(ids)
	slices.Reverse(titles)
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(from.ID, []string{}, ids), admin, http.StatusOK, nil)
	if got := kbCollectionTitles(t, api, admin, from.ID); fmt.Sprint(got) != fmt.Sprint(titles) {
		t.Fatalf("hand order = %v", got)
	}

	moved := kbBulkAction(t, api, admin, map[string]any{"action": "move", "ids": ids, "collection_id": to.ID})
	if moved.Counts["done"] != 50 || len(kbCollectionTitles(t, api, admin, from.ID)) != 0 {
		t.Fatalf("bulk move of 50: counts=%v", moved.Counts)
	}
	undone, _ := kbBulkUndo(t, api, admin, moved.Operation.ID, http.StatusOK)
	if undone.Counts["undone"] != 50 {
		t.Fatalf("undo counts = %v, want 50 undone", undone.Counts)
	}
	if got := kbCollectionTitles(t, api, admin, from.ID); fmt.Sprint(got) != fmt.Sprint(titles) {
		t.Fatalf("after undo the collection lists %v, want the hand order %v", got, titles)
	}
	if got := kbCollectionTitles(t, api, admin, to.ID); len(got) != 0 {
		t.Fatalf("still in the other collection after undo: %v", got)
	}
}

// Trashing published articles in bulk takes them off the help center; undo brings them back published.
func TestUndoingABulkTrashRestoresPublishedArticles(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Trash", "admin@kb-bulk-trash.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	first := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"})
	second := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Shipping", "body_md": "Two days.", "status": "published"})
	trashed := kbBulkAction(t, api, admin, map[string]any{"action": "trash", "ids": []string{first.ID, second.ID}})
	if trashed.Counts["done"] != 2 {
		t.Fatalf("bulk trash: %v", trashed.outcomes())
	}
	if got := kbCollectionTitles(t, api, admin, collection.ID); len(got) != 0 {
		t.Fatalf("trashed articles still listed: %v", got)
	}
	undone, _ := kbBulkUndo(t, api, admin, trashed.Operation.ID, http.StatusOK)
	if undone.Counts["undone"] != 2 {
		t.Fatalf("undo of the trash: %v", undone.outcomes())
	}
	for _, id := range []string{first.ID, second.ID} {
		if got := kbGetArticle(t, api, admin, id); got.Status != "published" || got.DeletedAt != nil {
			t.Fatalf("%s after undo: status=%s deleted=%v", id, got.Status, got.DeletedAt)
		}
	}
}

// Undo puts a published article back on the help center only for a caller who may publish.
func TestUndoingABulkTrashOfPublishedArticlesNeedsPublish(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "KB Bulk Trash Scope", "admin@kb-bulk-trash-scope.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	live := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Refunds", "body_md": "Within 14 days.", "status": "published"})
	trashed := kbBulkAction(t, api, admin, map[string]any{"action": "trash", "ids": []string{live.ID}})
	editor := issueCustomToken(t, api, admin, "Docs bot", "kb_articles:read", "kb_articles:update", "kb_articles:delete")
	undone, _ := kbBulkUndo(t, api, editor.Token, trashed.Operation.ID, http.StatusOK)
	if got := undone.outcomes()[live.ID]; got != "permission_missing" || undone.Counts["not_undone"] != 1 {
		t.Fatalf("undo without publish: %v counts=%v, want permission_missing", undone.outcomes(), undone.Counts)
	}
	var trash struct {
		Items []kbArticleView `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/trash", nil, admin, http.StatusOK, &trash)
	if len(trash.Items) != 1 || trash.Items[0].ID != live.ID {
		t.Fatalf("the article left the trash without publish: %d in the trash", len(trash.Items))
	}
}
