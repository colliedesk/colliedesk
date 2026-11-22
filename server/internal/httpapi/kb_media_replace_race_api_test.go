package httpapi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A replacement in one article reads the article's text only once it holds the article: a save that
// drops the image while the replacement waits wins, and the replacement is refused with 409, naming
// why, without recording anything to undo.
func TestReplacingInAnArticleThatDroppedTheImageMeanwhileIsRefused(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Replace Race", "admin@kb-replace-race.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	original := uploadKBMedia(t, api, admin, pngImage(186), nil)
	annotated := uploadKBMedia(t, api, admin, pngImage(187), nil)
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Race", "body_md": "See ![a](" + original.URL + ")"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	save, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = save.Rollback(ctx) }()
	if _, err := save.Exec(ctx, `SELECT 1 FROM kb_articles WHERE id=$1 FOR UPDATE`, article.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(t, api, http.MethodPost, "/api/v1/kb/media/"+original.ID+"/replace", map[string]any{"with_media_id": annotated.ID, "article_id": article.ID}, admin, nil)
	}()
	for waiting := false; !waiting; time.Sleep(10 * time.Millisecond) {
		// A row lock wait shows as a transactionid lock, which names no database: find it by backend.
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid
			WHERE NOT l.granted AND a.datname=current_database())`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		select {
		case response := <-done:
			t.Fatalf("the replacement finished without waiting for the article: %d %s", response.Code, response.Body.String())
		default:
		}
		if ctx.Err() != nil {
			t.Fatal("the replacement never waited for the article")
		}
	}
	if _, err := save.Exec(ctx, `UPDATE kb_articles SET body_md='No image now' WHERE id=$1`, article.ID); err != nil {
		t.Fatal(err)
	}
	if err := save.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	response := <-done
	if response.Code != http.StatusConflict || errorCode(t, response) != "media_not_in_article" || !strings.Contains(response.Body.String(), "does not show") {
		t.Fatalf("replacement = %d %s, want 409 media_not_in_article", response.Code, response.Body.String())
	}
	var recorded int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_media_replacements WHERE from_media_id=$1`, original.ID).Scan(&recorded); err != nil || recorded != 0 {
		t.Fatalf("replacements recorded = %d (%v), want 0", recorded, err)
	}
}
