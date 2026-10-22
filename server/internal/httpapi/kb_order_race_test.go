package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// slowKBWrite makes every row update of table that meets when (a trigger condition over OLD and NEW) wait
// sleep before its statement returns, still holding the row locks it took. Two writes that meet only
// in a window of a millisecond then meet on every run.
func slowKBWrite(t *testing.T, pool *pgxpool.Pool, table, when string, sleep time.Duration) {
	t.Helper()
	name := "slow_" + table
	if _, err := pool.Exec(context.Background(), fmt.Sprintf(`CREATE FUNCTION %[1]s() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(%[2]f); RETURN NULL; END $$;
		CREATE TRIGGER %[1]s AFTER UPDATE ON %[3]s FOR EACH ROW WHEN (%[4]s) EXECUTE FUNCTION %[1]s()`, name, sleep.Seconds(), table, when)); err != nil {
		t.Fatal(err)
	}
}

// race starts first, then second a moment later, and waits for both.
func race(api http.Handler, first, second *http.Request) (*httpResult, *httpResult) {
	var wait sync.WaitGroup
	var a, b *httpResult
	wait.Add(2)
	go func() { defer wait.Done(); a = serveResult(api, first) }()
	go func() { defer wait.Done(); time.Sleep(30 * time.Millisecond); b = serveResult(api, second) }()
	wait.Wait()
	return a, b
}

// articleOrder lists the articles of a collection as "title:position", in the order the API gives.
func articleOrder(t *testing.T, api http.Handler, token, collection string) []string {
	t.Helper()
	var page struct {
		Items []struct {
			Title    string `json:"title"`
			Position int    `json:"position"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles?limit=200&collection_id="+collection, nil, token, http.StatusOK, &page)
	out := []string{}
	for _, item := range page.Items {
		out = append(out, fmt.Sprintf("%s:%d", item.Title, item.Position))
	}
	return out
}

func kbArticleNow(t *testing.T, api http.Handler, token, id string) kbArticleView {
	t.Helper()
	var article kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+id, nil, token, http.StatusOK, &article)
	return article
}

// Reordering a collection while an article of it moves into one of its own collections: the reorder
// updates the collections' rows and the articles' rows, the move holds the article and then reads the
// collection it goes to. Both always finish: neither is aborted as a deadlock (40P01), which the API
// answers 409 conflict.
func TestKnowledgeBaseOrderAndAMoveIntoAChildCollectionNeverDeadlock(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Order Deadlock", "admin@order-deadlock.example.com")
	parent := kbCollection(t, api, admin, map[string]any{"name": "Parent"})
	first := kbCollection(t, api, admin, map[string]any{"name": "First", "parent_id": parent.ID})
	second := kbCollection(t, api, admin, map[string]any{"name": "Second", "parent_id": parent.ID})
	moving := kbArticle(t, api, admin, map[string]any{"collection_id": parent.ID, "title": "Moving", "body_md": "m"})
	staying := kbArticle(t, api, admin, map[string]any{"collection_id": parent.ID, "title": "Staying", "body_md": "s"})
	slowKBWrite(t, pool, "kb_collections", "OLD.position IS DISTINCT FROM NEW.position", 150*time.Millisecond)
	collections := []string{first.ID, second.ID}
	for round := range 6 {
		slices.Reverse(collections)
		article := kbArticleNow(t, api, admin, moving.ID)
		reorder := newRequest(t, http.MethodPut, "/api/v1/kb/order", kbOrderBody(parent.ID, collections, []string{moving.ID, staying.ID}), admin, nil)
		move := newRequest(t, http.MethodPatch, "/api/v1/kb/articles/"+moving.ID, map[string]any{"updated_at": article.UpdatedAt, "collection_id": second.ID}, admin, nil)
		ordered, moved := race(api, reorder, move)
		// A deadlock is answered 409 "conflict" (try again); only order_stale is a fair answer here.
		if ordered.code != http.StatusOK && (ordered.code != http.StatusConflict || !strings.Contains(string(ordered.body), `"order_stale"`)) {
			t.Fatalf("round %d: reorder status=%d body=%s", round, ordered.code, ordered.body)
		}
		if moved.code != http.StatusOK {
			t.Fatalf("round %d: move status=%d body=%s", round, moved.code, moved.body)
		}
		back := kbArticleNow(t, api, admin, moving.ID)
		kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+moving.ID, map[string]any{"updated_at": back.UpdatedAt, "collection_id": parent.ID}, admin, http.StatusOK, nil)
	}
}

// An order of a collection read while an article was moving out of it never writes its old place into
// the collection it went to: the article goes last there.
func TestKnowledgeBaseOrderNeverWritesAStalePlaceIntoAnotherCollection(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Order Stale Place", "admin@order-stale-place.example.com")
	from := kbCollection(t, api, admin, map[string]any{"name": "From"})
	to := kbCollection(t, api, admin, map[string]any{"name": "To"})
	moving := kbArticle(t, api, admin, map[string]any{"collection_id": from.ID, "title": "Moving", "body_md": "m"})
	other := kbArticle(t, api, admin, map[string]any{"collection_id": from.ID, "title": "Other", "body_md": "o"})
	there := []kbArticleView{
		kbArticle(t, api, admin, map[string]any{"collection_id": to.ID, "title": "Delta", "body_md": "d"}),
		kbArticle(t, api, admin, map[string]any{"collection_id": to.ID, "title": "Echo", "body_md": "e"}),
	}
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(to.ID, []string{}, []string{there[0].ID, there[1].ID}), admin, http.StatusOK, nil)
	slowKBWrite(t, pool, "kb_articles", "OLD.collection_id IS DISTINCT FROM NEW.collection_id", 150*time.Millisecond)
	move := newRequest(t, http.MethodPatch, "/api/v1/kb/articles/"+moving.ID, map[string]any{"updated_at": moving.UpdatedAt, "collection_id": to.ID}, admin, nil)
	reorder := newRequest(t, http.MethodPut, "/api/v1/kb/order", kbOrderBody(from.ID, []string{}, []string{moving.ID, other.ID}), admin, nil)
	moved, ordered := race(api, move, reorder)
	if moved.code != http.StatusOK || ordered.code != http.StatusConflict {
		t.Fatalf("move status=%d, reorder status=%d body=%s; want 200 and 409 order_stale", moved.code, ordered.code, ordered.body)
	}
	if got := articleOrder(t, api, admin, to.ID); !slices.Equal(got, []string{"Delta:1", "Echo:2", "Moving:3"}) {
		t.Fatalf("collection the article moved to = %v, want it last", got)
	}
}

// An article moved into a collection while the collection gets its first order by hand goes last, not
// first with the place 0 it read before the order was saved.
func TestKnowledgeBaseAMoveDuringAFirstOrderGoesLast(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Order First", "admin@order-first.example.com")
	target := kbCollection(t, api, admin, map[string]any{"name": "Target"})
	elsewhere := kbCollection(t, api, admin, map[string]any{"name": "Elsewhere"})
	alpha := kbArticle(t, api, admin, map[string]any{"collection_id": target.ID, "title": "Alpha", "body_md": "a"})
	bravo := kbArticle(t, api, admin, map[string]any{"collection_id": target.ID, "title": "Bravo", "body_md": "b"})
	moving := kbArticle(t, api, admin, map[string]any{"collection_id": elsewhere.ID, "title": "Aardvark", "body_md": "m"})
	slowKBWrite(t, pool, "kb_articles", "OLD.position IS DISTINCT FROM NEW.position AND OLD.collection_id = NEW.collection_id", 150*time.Millisecond)
	reorder := newRequest(t, http.MethodPut, "/api/v1/kb/order", kbOrderBody(target.ID, []string{}, []string{bravo.ID, alpha.ID}), admin, nil)
	move := newRequest(t, http.MethodPatch, "/api/v1/kb/articles/"+moving.ID, map[string]any{"updated_at": moving.UpdatedAt, "collection_id": target.ID}, admin, nil)
	ordered, moved := race(api, reorder, move)
	if ordered.code != http.StatusOK || moved.code != http.StatusOK {
		t.Fatalf("reorder status=%d body=%s, move status=%d body=%s", ordered.code, ordered.body, moved.code, moved.body)
	}
	if got := articleOrder(t, api, admin, target.ID); !slices.Equal(got, []string{"Bravo:1", "Alpha:2", "Aardvark:3"}) {
		t.Fatalf("collection after the first order and the move = %v, want the moved article last", got)
	}
}

// helpCenterVersion is the content_version help center pages are cached under.
func helpCenterVersion(t *testing.T, api http.Handler, token string) int {
	t.Helper()
	var center struct {
		ContentVersion int `json:"content_version"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/help-center", nil, token, http.StatusOK, &center)
	return center.ContentVersion
}

func TestKnowledgeBaseOrderThatChangesNothingKeepsTheCachedPages(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Order Same", "admin@order-same.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	one := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "One", "body_md": "1"})
	two := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Two", "body_md": "2"})
	body := kbOrderBody(collection.ID, []string{}, []string{two.ID, one.ID})
	before := helpCenterVersion(t, api, admin)
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", body, admin, http.StatusOK, nil)
	ordered := helpCenterVersion(t, api, admin)
	if ordered <= before {
		t.Fatalf("content_version after a new order = %d, before %d: cached pages keep the old order", ordered, before)
	}
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", body, admin, http.StatusOK, nil)
	if again := helpCenterVersion(t, api, admin); again != ordered {
		t.Fatalf("content_version after the same order again = %d, want %d: nothing moved", again, ordered)
	}
}

// trashAndRestoreArticle moves an article to the trash and takes it out again.
func trashAndRestoreArticle(t *testing.T, api http.Handler, token, id string) {
	t.Helper()
	deleteArticle(t, api, token, id)
	restoreArticle(t, api, token, id)
}

func restoreArticle(t *testing.T, api http.Handler, token, id string) {
	t.Helper()
	var trash struct {
		Items []kbTrashItem `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/trash", nil, token, http.StatusOK, &trash)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+id+"/restore", map[string]any{"updated_at": trashUpdatedAt(trash.Items, id)}, token, http.StatusOK, nil)
}

func deleteArticle(t *testing.T, api http.Handler, token, id string) {
	t.Helper()
	article := kbArticleNow(t, api, token, id)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+id+"?updated_at="+url.QueryEscape(article.UpdatedAt), nil, token, http.StatusNoContent, nil)
}

func TestKnowledgeBaseAnArticleBackFromTheTrashGoesLastInAnOrderedCollection(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Order Restore", "admin@order-restore.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	plain := kbCollection(t, api, admin, map[string]any{"name": "Plain"})
	add := func(in kbCollectionView, title string) kbArticleView {
		return kbArticle(t, api, admin, map[string]any{"collection_id": in.ID, "title": title, "body_md": title})
	}
	alpha, bravo, charlie, delta := add(collection, "Alpha"), add(collection, "Bravo"), add(collection, "Charlie"), add(collection, "Delta")
	// Trashed before the collection was ordered: it still has place 0, which would put it first.
	early := add(collection, "Aaa early")
	deleteArticle(t, api, admin, early.ID)
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(collection.ID, []string{}, []string{alpha.ID, bravo.ID, charlie.ID, delta.ID}), admin, http.StatusOK, nil)
	// Bravo leaves at place 2; the collection is ordered again without it, so Charlie takes place 2.
	deleteArticle(t, api, admin, bravo.ID)
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(collection.ID, []string{}, []string{alpha.ID, charlie.ID, delta.ID}), admin, http.StatusOK, nil)
	restoreArticle(t, api, admin, bravo.ID)
	restoreArticle(t, api, admin, early.ID)
	if got := articleOrder(t, api, admin, collection.ID); !slices.Equal(got, []string{"Alpha:1", "Charlie:2", "Delta:3", "Bravo:4", "Aaa early:5"}) {
		t.Fatalf("ordered collection after two restores = %v, want both last in the order restored", got)
	}
	// A collection never ordered by hand keeps the title order: the restored article falls in by title.
	add(plain, "Zulu")
	mike := add(plain, "Mike")
	trashAndRestoreArticle(t, api, admin, mike.ID)
	if got := articleOrder(t, api, admin, plain.ID); !slices.Equal(got, []string{"Mike:0", "Zulu:0"}) {
		t.Fatalf("unordered collection after a restore = %v", got)
	}
}

// childOrder lists the names of the collections under parent ("" for the top) as "name:position", in
// the order the API gives.
func childOrder(t *testing.T, api http.Handler, token, parent string) []string {
	t.Helper()
	var list struct {
		Items []struct {
			Name     string  `json:"name"`
			ParentID *string `json:"parent_id"`
			Position int     `json:"position"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections", nil, token, http.StatusOK, &list)
	out := []string{}
	for _, item := range list.Items {
		if (item.ParentID == nil && parent == "") || (item.ParentID != nil && *item.ParentID == parent) {
			out = append(out, fmt.Sprintf("%s:%d", item.Name, item.Position))
		}
	}
	return out
}

func trashAndRestoreCollection(t *testing.T, api http.Handler, token string, collection kbCollectionView) {
	t.Helper()
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/collections/"+collection.ID+"?updated_at="+url.QueryEscape(collection.UpdatedAt), nil, token, http.StatusNoContent, nil)
	var trash struct {
		Items []struct {
			ID        string `json:"id"`
			UpdatedAt string `json:"updated_at"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections/trash", nil, token, http.StatusOK, &trash)
	for _, item := range trash.Items {
		if item.ID == collection.ID {
			kbCall(t, api, http.MethodPost, "/api/v1/kb/collections/"+collection.ID+"/restore", map[string]any{"updated_at": item.UpdatedAt}, token, http.StatusOK, nil)
			return
		}
	}
	t.Fatalf("collection %s is not in the trash", collection.Name)
}

func TestKnowledgeBaseACollectionBackFromTheTrashGoesLastAmongOrderedSiblings(t *testing.T) {
	api, service := testAPI(t)
	admin := createSession(t, api, service, "Order Restore Top", "admin@order-restore-top.example.com")
	alpha := kbCollection(t, api, admin, map[string]any{"name": "Alpha"})
	bravo := kbCollection(t, api, admin, map[string]any{"name": "Bravo"})
	charlie := kbCollection(t, api, admin, map[string]any{"name": "Charlie"})
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(nil, []string{charlie.ID, bravo.ID, alpha.ID}, []string{}), admin, http.StatusOK, nil)
	// Bravo leaves at place 2 and the top is ordered again without it: Alpha takes place 2.
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/collections/"+bravo.ID+"?updated_at="+url.QueryEscape(bravo.UpdatedAt), nil, admin, http.StatusNoContent, nil)
	kbCall(t, api, http.MethodPut, "/api/v1/kb/order", kbOrderBody(nil, []string{charlie.ID, alpha.ID}, []string{}), admin, http.StatusOK, nil)
	var trash struct {
		Items []struct {
			UpdatedAt string `json:"updated_at"`
		} `json:"items"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/collections/trash", nil, admin, http.StatusOK, &trash)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/collections/"+bravo.ID+"/restore", map[string]any{"updated_at": trash.Items[0].UpdatedAt}, admin, http.StatusOK, nil)
	if got := childOrder(t, api, admin, ""); !slices.Equal(got, []string{"Charlie:1", "Alpha:2", "Bravo:3"}) {
		t.Fatalf("top after a restore = %v, want the restored collection last", got)
	}
	// Under a parent never ordered by hand the restored collection falls in by name.
	inside := []kbCollectionView{
		kbCollection(t, api, admin, map[string]any{"name": "Zulu", "parent_id": alpha.ID}),
		kbCollection(t, api, admin, map[string]any{"name": "Mike", "parent_id": alpha.ID}),
	}
	trashAndRestoreCollection(t, api, admin, inside[1])
	if got := childOrder(t, api, admin, alpha.ID); !slices.Equal(got, []string{"Mike:0", "Zulu:0"}) {
		t.Fatalf("unordered parent after a restore = %v", got)
	}
}
