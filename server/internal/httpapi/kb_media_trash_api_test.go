package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

type kbMediaResults struct {
	Results []struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	} `json:"results"`
	BatchID *string `json:"batch_id"`
}

func (r kbMediaResults) status(id string) string {
	for _, result := range r.Results {
		if result.ID == id {
			return result.Status
		}
	}
	return ""
}

// An item a live article shows stays out of the trash; an unused one goes, its batch brings it back,
// and saving an article that names an item in the trash takes it out again.
func TestKBMediaTrashKeepsWhatArticlesShowAndUndoesInBatches(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Trash", "admin@kb-media-trash.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	unused := uploadKBMedia(t, api, admin, pngImage(40), url.Values{"name": {"Unused"}})
	shown := uploadKBMedia(t, api, admin, pngImage(41), nil)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Shows", "body_md": "![a](" + shown.URL + ")"})
	missing := "00000000-0000-4000-8000-000000000001"

	var moved kbMediaResults
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/trash", map[string]any{"ids": []string{unused.ID, shown.ID, missing}}, admin, http.StatusOK, &moved)
	if moved.status(unused.ID) != "trashed" || moved.status(shown.ID) != "in_use" || moved.status(missing) != "not_found" || moved.BatchID == nil {
		t.Fatalf("trash = %+v", moved)
	}
	var again kbMediaResults
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/trash", map[string]any{"ids": []string{unused.ID}}, admin, http.StatusOK, &again)
	if again.status(unused.ID) != "already_in_trash" || again.BatchID != nil {
		t.Fatalf("trash again = %+v", again)
	}
	var trash kbMediaPage
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/trash", nil, admin, http.StatusOK, &trash)
	if len(trash.Items) != 1 || trash.Items[0].ID != unused.ID || trash.Items[0].PurgeAt == nil {
		t.Fatalf("trash list = %+v", trash.Items)
	}
	for _, item := range kbMediaList(t, api, admin, "").Items {
		if item.ID == unused.ID {
			t.Fatal("an item in the trash is in the library list")
		}
	}

	// DELETE refuses an item in use with the articles that show it.
	refused := kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+shown.ID, nil, admin, http.StatusConflict, nil)
	var body struct {
		Error struct {
			Code     string `json:"code"`
			Articles []struct {
				ID string `json:"id"`
			} `json:"articles"`
		} `json:"error"`
	}
	decode(t, refused, &body)
	if body.Error.Code != "media_in_use" || len(body.Error.Articles) != 1 || body.Error.Articles[0].ID != article.ID {
		t.Fatalf("delete in use = %s", refused.Body.String())
	}

	// The batch's Undo brings the item back.
	var restored kbMediaResults
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/restore", map[string]any{"batch_id": *moved.BatchID}, admin, http.StatusOK, &restored)
	if restored.status(unused.ID) != "restored" {
		t.Fatalf("undo = %+v", restored)
	}
	// Saving an article that names an item in the trash takes it out.
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+unused.ID, nil, admin, http.StatusOK, nil)
	kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Names it", "body_md": "![b](" + unused.URL + ")"})
	var details kbMediaView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+unused.ID+"/details", nil, admin, http.StatusOK, &details)
	if details.Usage == nil || details.Usage.Articles != 1 {
		t.Fatalf("after the save = %+v", details)
	}
	if response := request(t, api, http.MethodPost, "/api/v1/kb/media/"+unused.ID+"/restore", nil, admin, nil); response.Code != http.StatusNotFound {
		t.Fatalf("restore of an item outside the trash status=%d", response.Code)
	}
}

// "Move all unused" leaves kept items and items added after the page was read, works in batches of one
// Undo, and a token below send gets a request that names the ids.
func TestKBMediaTrashAllUnused(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Unused", "admin@kb-media-unused.example.com")
	first := uploadKBMedia(t, api, admin, pngImage(50), nil)
	second := uploadKBMedia(t, api, admin, pngImage(51), nil)
	kept := uploadKBMedia(t, api, admin, pngImage(52), url.Values{"keep": {"true"}})
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE kb_media SET created_at=now()-interval '1 hour'`); err != nil {
		t.Fatal(err)
	}
	before := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	later := uploadKBMedia(t, api, admin, pngImage(53), nil)

	reader := issueToken(t, api, admin, "reader", "read").Token
	proposed := request(t, api, http.MethodPost, "/api/v1/kb/media/trash-unused", map[string]any{"before": before}, reader, nil)
	if proposed.Code != http.StatusAccepted {
		t.Fatalf("read token status=%d body=%s", proposed.Code, proposed.Body.String())
	}
	var filed struct {
		RequestID string `json:"request_id"`
	}
	decode(t, proposed, &filed)
	var requestView struct {
		Route string          `json:"route"`
		Body  json.RawMessage `json:"body"`
	}
	kbCall(t, api, http.MethodGet, "/api/v1/agent-requests/"+filed.RequestID, nil, admin, http.StatusOK, &requestView)
	var ids struct {
		IDs []string `json:"ids"`
	}
	if err := json.Unmarshal(requestView.Body, &ids); err != nil || requestView.Route != "/api/v1/kb/media/trash" || len(ids.IDs) != 2 {
		t.Fatalf("filed request = %s %s", requestView.Route, requestView.Body)
	}

	var run struct {
		Trashed int     `json:"trashed"`
		More    bool    `json:"more"`
		BatchID *string `json:"batch_id"`
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/trash-unused", map[string]any{"before": before}, admin, http.StatusOK, &run)
	if run.Trashed != 2 || run.More || run.BatchID == nil {
		t.Fatalf("trash unused = %+v", run)
	}
	batch := *run.BatchID
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/trash-unused", map[string]any{"before": before, "include_kept": true, "batch_id": batch}, admin, http.StatusOK, &run)
	if run.Trashed != 1 || *run.BatchID != batch {
		t.Fatalf("trash unused with kept = %+v", run)
	}
	var restored kbMediaResults
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/restore", map[string]any{"batch_id": batch}, admin, http.StatusOK, &restored)
	for _, item := range []kbMediaView{first, second, kept} {
		if restored.status(item.ID) != "restored" {
			t.Fatalf("undo of the batch = %+v", restored)
		}
	}
	if restored.status(later.ID) != "" {
		t.Fatal("an item uploaded after the page was read was in the batch")
	}
}

// Delete forever only takes items in the trash, removes their files, and leaves an article that named
// one saving with a warning.
func TestKBMediaPurgeNowDeletesOnlyTrashedItems(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Purge Now", "admin@kb-media-purge-now.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	media := uploadKBMedia(t, api, admin, pngImage(60), nil)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Old", "body_md": "![a](" + media.URL + ")"})
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+article.ID+"?updated_at="+url.QueryEscape(article.UpdatedAt), nil, admin, http.StatusNoContent, nil)

	var result kbMediaResults
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/purge", map[string]any{"ids": []string{media.ID}}, admin, http.StatusOK, &result)
	if result.status(media.ID) != "not_in_trash" {
		t.Fatalf("purge outside the trash = %+v", result)
	}
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+media.ID, nil, admin, http.StatusOK, nil)
	var key string
	if err := pool.QueryRow(context.Background(), `SELECT storage_key FROM kb_media WHERE id=$1`, media.ID).Scan(&key); err != nil {
		t.Fatal(err)
	}
	agent := issueToken(t, api, admin, "sender", "send").Token
	if response := request(t, api, http.MethodPost, "/api/v1/kb/media/purge", map[string]any{"ids": []string{media.ID}}, agent, nil); response.Code != http.StatusAccepted {
		t.Fatalf("send token purge status=%d, want an agent request", response.Code)
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/purge", map[string]any{"ids": []string{media.ID}}, admin, http.StatusOK, &result)
	if result.status(media.ID) != "purged" {
		t.Fatalf("purge = %+v", result)
	}
	if body, err := storage.Open(context.Background(), key); err == nil {
		body.Close()
		t.Fatal("the purged item's file is still stored")
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+media.ID+"/details", nil, admin, http.StatusNotFound, nil)
	var saved struct {
		MissingMedia []string `json:"missing_media"`
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles", map[string]any{"collection_id": collection.ID, "title": "Names a gone item", "body_md": "![a](" + media.URL + ")"},
		admin, http.StatusCreated, &saved)
	if len(saved.MissingMedia) != 1 || saved.MissingMedia[0] != media.ID {
		t.Fatalf("missing_media = %v", saved.MissingMedia)
	}
}

// The daily purge deletes what has been in the trash for 30 days.
func TestKBMediaTrashIsPurgedAfterThirtyDays(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Trash Purge", "admin@kb-media-trash-purge.example.com")
	media := uploadKBMedia(t, api, admin, pngImage(70), url.Values{"keep": {"true"}})
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+media.ID, nil, admin, http.StatusOK, nil)
	if _, err := pool.Exec(context.Background(), `UPDATE kb_media SET deleted_at=now()-interval '31 days'`); err != nil {
		t.Fatal(err)
	}
	purged, err := store.New(pool).WithObjects(storage).PurgeKBTrash(context.Background())
	if err != nil || purged.Images != 1 {
		t.Fatalf("purged = %+v (%v)", purged, err)
	}
}

// Another workspace's items, batches and trash answer as if they did not exist.
func TestKBMediaTrashNeverCrossesWorkspaces(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	alpha := createSession(t, api, service, "KB Media Trash Alpha", "admin@kb-media-trash-alpha.example.com")
	beta := createSession(t, api, service, "KB Media Trash Beta", "admin@kb-media-trash-beta.example.com")
	media := uploadKBMedia(t, api, alpha, pngImage(80), nil)
	var moved kbMediaResults
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/trash", map[string]any{"ids": []string{uploadKBMedia(t, api, alpha, pngImage(81), nil).ID}}, alpha, http.StatusOK, &moved)
	for _, path := range []string{"/api/v1/kb/media/trash", "/api/v1/kb/media/restore", "/api/v1/kb/media/purge"} {
		var result kbMediaResults
		kbCall(t, api, http.MethodPost, path, map[string]any{"ids": []string{media.ID}}, beta, http.StatusOK, &result)
		if result.status(media.ID) != "not_found" {
			t.Errorf("beta %s = %+v", path, result)
		}
	}
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+media.ID, nil, beta, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+media.ID+"/restore", nil, beta, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/restore", map[string]any{"batch_id": *moved.BatchID}, beta, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/trash", map[string]any{"ids": []string{uploadKBMedia(t, api, beta, pngImage(82), nil).ID}, "batch_id": *moved.BatchID},
		beta, http.StatusNotFound, nil)
	var trash kbMediaPage
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/trash", nil, beta, http.StatusOK, &trash)
	if len(trash.Items) != 0 {
		t.Fatalf("beta sees alpha's trash: %+v", trash.Items)
	}
	var run struct {
		Trashed int `json:"trashed"`
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/trash-unused", map[string]any{"before": "2100-01-01T00:00:00Z"}, beta, http.StatusOK, &run)
	var details kbMediaView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+media.ID+"/details", nil, alpha, http.StatusOK, &details)
	if run.Trashed != 1 || details.ID != media.ID {
		t.Fatalf("beta's trash-unused moved %d; alpha's item still live: %+v", run.Trashed, details)
	}
}

// Moving an item to the trash and saving an article that names it, at the same moment, 200 times:
// never an item in the trash that a live article's current text names.
func TestKBMediaTrashNeverHidesAnItemASaveIsLinking(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Trash Race", "admin@kb-media-trash-race.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Race"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Race", "body_md": "x"})
	seen := article.UpdatedAt
	ctx := context.Background()
	savedFirst, trashedFirst := 0, 0
	for round := range 200 {
		media := uploadKBMedia(t, api, admin, pngImage(byte(round)), nil)
		save := newRequest(t, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": seen, "body_md": "![x](" + media.URL + ")"}, admin, nil)
		trash := newRequest(t, http.MethodPost, "/api/v1/kb/media/trash", map[string]any{"ids": []string{media.ID}}, admin, nil)
		var wait sync.WaitGroup
		var saved, trashed *httpResult
		start := make(chan struct{})
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			saved = serveResult(api, save)
		}()
		go func() {
			defer wait.Done()
			<-start
			time.Sleep(time.Duration(round%13) * 500 * time.Microsecond)
			trashed = serveResult(api, trash)
		}()
		close(start)
		wait.Wait()
		if saved.code != http.StatusOK || trashed.code != http.StatusOK {
			t.Fatalf("round %d: save %d %s, trash %d %s", round, saved.code, saved.body, trashed.code, trashed.body)
		}
		var updated kbArticleView
		if err := json.Unmarshal(saved.body, &updated); err != nil {
			t.Fatal(err)
		}
		seen = updated.UpdatedAt
		var inTrash, linked bool
		if err := pool.QueryRow(ctx, `SELECT m.deleted_at IS NOT NULL, EXISTS(SELECT 1 FROM kb_article_media l WHERE l.media_id=m.id AND l.current)
			FROM kb_media m WHERE m.id=$1`, media.ID).Scan(&inTrash, &linked); err != nil {
			t.Fatal(err)
		}
		if inTrash && linked {
			t.Fatalf("round %d: %s is in the trash while the article's current text names it", round, media.ID)
		}
		var result kbMediaResults
		_ = json.Unmarshal(trashed.body, &result)
		if result.status(media.ID) == "trashed" {
			trashedFirst++
		} else {
			savedFirst++
		}
	}
	t.Logf("trash first: %d, save first: %d", trashedFirst, savedFirst)
}

// Taking an item nobody kept out of the trash gives it its 30 days again before the purge of unused items.
func TestKBMediaRestoreStartsTheThirtyDaysAgain(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Restore Days", "admin@kb-media-restore-days.example.com")
	media := uploadKBMedia(t, api, admin, pngImage(152), nil)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE kb_media SET last_uploaded_at=now()-interval '29 days' WHERE id=$1`, media.ID); err != nil {
		t.Fatal(err)
	}
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+media.ID, nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+media.ID+"/restore", nil, admin, http.StatusOK, nil)
	var days float64
	if err := pool.QueryRow(ctx, `SELECT extract(epoch from now()-last_uploaded_at)/86400 FROM kb_media WHERE id=$1`, media.ID).Scan(&days); err != nil || days > 1 {
		t.Fatalf("restored item last uploaded %.1f days ago (%v), want now", days, err)
	}
}
