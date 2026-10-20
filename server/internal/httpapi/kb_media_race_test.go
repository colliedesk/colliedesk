package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/store"
)

// A save that names an unused item and the daily purge run at the same moment 200 times. Either the
// purge goes first and the save finds the item gone, or the save links it and the purge keeps it: a
// link a save committed is never purged under it. A trigger records every link written, since the
// purge's cascade would remove the link itself.
func TestKBMediaPurgeNeverDeletesAnItemASaveJustLinked(t *testing.T) {
	api, service, pool, storage := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Race", "admin@kb-media-race.example.com")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE link_log(article_id uuid, media_id uuid);
		CREATE FUNCTION log_link() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO link_log VALUES (NEW.article_id, NEW.media_id); RETURN NULL; END $$;
		CREATE TRIGGER log_link AFTER INSERT ON kb_article_media FOR EACH ROW EXECUTE FUNCTION log_link();
		GRANT INSERT ON link_log TO PUBLIC`); err != nil {
		t.Fatal(err)
	}
	collection := kbCollection(t, api, admin, map[string]any{"name": "Race"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Race", "body_md": "x"})
	seen := article.UpdatedAt
	data := store.New(pool).WithObjects(storage)
	linked, purged := 0, 0
	for round := range 200 {
		media := uploadKBMedia(t, api, admin, pngImage(byte(round)), nil)
		if _, err := pool.Exec(ctx, `UPDATE kb_media SET last_uploaded_at=now()-interval '31 days' WHERE id=$1`, media.ID); err != nil {
			t.Fatal(err)
		}
		var wait sync.WaitGroup
		var saved *httpResult
		var purgeErr error
		start := make(chan struct{})
		save := newRequest(t, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"updated_at": seen, "body_md": "![x](" + media.URL + ")"}, admin, nil)
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			saved = serveResult(api, save)
		}()
		go func() {
			defer wait.Done()
			<-start
			// The save takes a few milliseconds; starting the purge up to 6 ms later meets it at every step.
			time.Sleep(time.Duration(round%13) * 500 * time.Microsecond)
			_, purgeErr = data.PurgeKBTrash(ctx)
		}()
		close(start)
		wait.Wait()
		if purgeErr != nil || saved.code != http.StatusOK {
			t.Fatalf("round %d: purge %v, save %d %s", round, purgeErr, saved.code, saved.body)
		}
		var updated kbArticleView
		if err := json.Unmarshal(saved.body, &updated); err != nil {
			t.Fatal(err)
		}
		seen = updated.UpdatedAt
		var logged, exists bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM link_log WHERE media_id=$1), EXISTS(SELECT 1 FROM kb_media WHERE id=$1)`, media.ID).Scan(&logged, &exists); err != nil {
			t.Fatal(err)
		}
		switch {
		case logged && !exists:
			t.Fatalf("round %d: the save linked %s and the purge deleted it", round, media.ID)
		case logged:
			linked++
		case !exists:
			purged++
		}
	}
	t.Logf("saves first: %d, purges first: %d", linked, purged)
}

// Two saves that name the same item in the library's trash take it out together, never deadlock.
func TestKBMediaTwoSavesTakeTheSameItemOutOfTheTrash(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Trash Race", "admin@kb-media-trash-race.example.com")
	ctx := context.Background()
	collection := kbCollection(t, api, admin, map[string]any{"name": "Race"})
	first := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "One", "body_md": "x"})
	second := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Two", "body_md": "x"})
	seen := map[string]string{first.ID: first.UpdatedAt, second.ID: second.UpdatedAt}
	for round := range 50 {
		media := uploadKBMedia(t, api, admin, pngImage(byte(round)), nil)
		if _, err := pool.Exec(ctx, `UPDATE kb_media SET deleted_at=now() WHERE id=$1`, media.ID); err != nil {
			t.Fatal(err)
		}
		var wait sync.WaitGroup
		results := map[string]*httpResult{}
		var mu sync.Mutex
		start := make(chan struct{})
		for id, updatedAt := range seen {
			save := newRequest(t, http.MethodPatch, "/api/v1/kb/articles/"+id, map[string]any{"updated_at": updatedAt, "body_md": "![x](" + media.URL + ") " + id}, admin, nil)
			wait.Add(1)
			go func() {
				defer wait.Done()
				<-start
				result := serveResult(api, save)
				mu.Lock()
				results[id] = result
				mu.Unlock()
			}()
		}
		close(start)
		wait.Wait()
		for id, result := range results {
			if result.code != http.StatusOK {
				t.Fatalf("round %d: save %d %s", round, result.code, result.body)
			}
			var updated kbArticleView
			if err := json.Unmarshal(result.body, &updated); err != nil {
				t.Fatal(err)
			}
			seen[id] = updated.UpdatedAt
		}
		var trashed bool
		if err := pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM kb_media WHERE id=$1`, media.ID).Scan(&trashed); err != nil || trashed {
			t.Fatalf("round %d: still in the trash (%v)", round, err)
		}
	}
}

type httpResult struct {
	code int
	body []byte
}

func serveResult(api http.Handler, req *http.Request) *httpResult {
	response := serve(api, req)
	return &httpResult{code: response.Code, body: response.Body.Bytes()}
}
