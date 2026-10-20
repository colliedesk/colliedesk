package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type kbReplacementView struct {
	ID           string   `json:"id"`
	From         string   `json:"from_media_id"`
	To           string   `json:"to_media_id"`
	ArticleIDs   []string `json:"article_ids"`
	Translations []struct {
		ArticleID string `json:"article_id"`
		Locale    string `json:"locale"`
	} `json:"translations"`
	UndoneAt *string `json:"undone_at"`
}

// Replacing an image puts the new one in every live article that shows the old one, its text and its
// translations, as a new version; undoing it puts the old one back once.
func TestKBMediaReplaceRewritesEveryArticleAndUndoes(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Media Replace", "admin@kb-media-replace.example.com")
	ctx := context.Background()
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	old := uploadKBMedia(t, api, admin, pngImage(90), nil)
	replacement := uploadKBMedia(t, api, admin, pngImage(91), nil)
	shows := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Shows it", "status": "published", "body_md": "Before ![a](" + old.URL + ") after"})
	translated := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Translated", "body_md": "No image here"})
	if _, err := pool.Exec(ctx, `INSERT INTO kb_article_translations(workspace_id,article_id,locale,title,body_md)
		SELECT workspace_id,id,'de','Übersetzt','![b](/api/v1/kb/images/'||$2||')' FROM kb_articles WHERE id=$1`, translated.ID, strings.ToUpper(old.ID)); err != nil {
		t.Fatal(err)
	}
	// A save links what the translation shows.
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+translated.ID, map[string]any{"updated_at": translated.UpdatedAt, "title": "Translated too"}, admin, http.StatusOK, nil)

	var done kbReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+old.ID+"/replace", map[string]any{"with_media_id": replacement.ID}, admin, http.StatusOK, &done)
	if len(done.ArticleIDs) != 2 || len(done.Translations) != 1 || done.Translations[0].Locale != "de" || done.UndoneAt != nil {
		t.Fatalf("replacement = %+v", done)
	}
	var after kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+shows.ID, nil, admin, http.StatusOK, &after)
	if after.BodyMD != "Before ![a]("+replacement.URL+") after" {
		t.Fatalf("body after replace = %q", after.BodyMD)
	}
	var kind string
	if err := pool.QueryRow(ctx, `SELECT kind FROM kb_article_versions WHERE article_id=$1 ORDER BY version DESC LIMIT 1`, shows.ID).Scan(&kind); err != nil || kind != "media_replace" {
		t.Fatalf("last version kind = %q (%v)", kind, err)
	}
	var translation string
	if err := pool.QueryRow(ctx, `SELECT body_md FROM kb_article_translations WHERE article_id=$1`, translated.ID).Scan(&translation); err != nil || !strings.Contains(translation, replacement.ID) {
		t.Fatalf("translation after replace = %q (%v)", translation, err)
	}
	var usage kbMediaView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+old.ID+"/details", nil, admin, http.StatusOK, &usage)
	if usage.Usage == nil || usage.Usage.Articles != 0 || usage.Usage.OlderVersions != 2 {
		t.Fatalf("old item usage = %+v", usage.Usage)
	}

	var undone kbReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, admin, http.StatusOK, &undone)
	if undone.UndoneAt == nil {
		t.Fatal("undo did not mark the replacement")
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+shows.ID, nil, admin, http.StatusOK, &after)
	if after.BodyMD != "Before ![a]("+old.URL+") after" {
		t.Fatalf("body after undo = %q", after.BodyMD)
	}
	var versions int
	countVersions := func() int {
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM kb_article_versions WHERE article_id=$1`, shows.ID).Scan(&versions); err != nil {
			t.Fatal(err)
		}
		return versions
	}
	before := countVersions()
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, admin, http.StatusOK, nil)
	if countVersions() != before {
		t.Fatal("a second undo wrote a version")
	}

	// The export carries the replacement and its undo.
	exported := request(t, api, http.MethodGet, "/api/v1/export", nil, admin, nil)
	if exported.Code != http.StatusOK {
		t.Fatalf("export status=%d", exported.Code)
	}
	if lines := ndjsonLines(t, unzip(t, exported.Body.Bytes())["kb-media-replacements.ndjson"]); len(lines) != 1 || lines[0]["undone_at"] == nil || lines[0]["from_media_id"] != old.ID {
		t.Fatalf("kb-media-replacements.ndjson = %v", lines)
	}

	// An image is replaced by an image only.
	if _, err := pool.Exec(ctx, `INSERT INTO kb_media(id,workspace_id,kind,storage_key,sha256,size,content_type)
		SELECT '00000000-0000-4000-8000-0000000000aa',workspace_id,'video',storage_key,repeat('e',64),10,'video/mp4' FROM kb_media WHERE id=$1`, old.ID); err != nil {
		t.Fatal(err)
	}
	if response := request(t, api, http.MethodPost, "/api/v1/kb/media/"+old.ID+"/replace", map[string]any{"with_media_id": "00000000-0000-4000-8000-0000000000aa"}, admin, nil); response.Code != http.StatusBadRequest {
		t.Fatalf("image replaced by a video status=%d", response.Code)
	}

	// Once the replaced item is deleted for good, the replacement cannot be undone.
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+old.ID+"/replace", map[string]any{"with_media_id": replacement.ID}, admin, http.StatusOK, &done)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/media/"+old.ID, nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/purge", map[string]any{"ids": []string{old.ID}}, admin, http.StatusOK, nil)
	gone := kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, admin, http.StatusConflict, nil)
	if errorCode(t, gone) != "media_gone" {
		t.Fatalf("undo after purge = %s", gone.Body.String())
	}
}

// Another workspace's items and replacements answer as if they did not exist.
func TestKBMediaReplaceNeverCrossesWorkspaces(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	alpha := createSession(t, api, service, "KB Replace Alpha", "admin@kb-replace-alpha.example.com")
	beta := createSession(t, api, service, "KB Replace Beta", "admin@kb-replace-beta.example.com")
	alphaOld := uploadKBMedia(t, api, alpha, pngImage(92), nil)
	alphaNew := uploadKBMedia(t, api, alpha, pngImage(93), nil)
	betaOwn := uploadKBMedia(t, api, beta, pngImage(94), nil)
	var done kbReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+alphaOld.ID+"/replace", map[string]any{"with_media_id": alphaNew.ID}, alpha, http.StatusOK, &done)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+alphaOld.ID+"/replace", map[string]any{"with_media_id": betaOwn.ID}, beta, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+betaOwn.ID+"/replace", map[string]any{"with_media_id": alphaNew.ID}, beta, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+alphaOld.ID+"/replace", map[string]any{"with_media_id": betaOwn.ID}, alpha, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, beta, http.StatusNotFound, nil)
}

// An undo puts back the text the article had, not every link of the new item: an article that already
// showed the new item keeps it, and an article changed since the replacement keeps its text and is
// listed as skipped.
func TestKBMediaReplaceUndoPutsBackOnlyWhatTheReplacementChanged(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Replace Undo", "admin@kb-replace-undo.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	from := uploadKBMedia(t, api, admin, pngImage(150), nil)
	to := uploadKBMedia(t, api, admin, pngImage(151), nil)
	both := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Both", "body_md": "![o](" + from.URL + ") and ![n](" + to.URL + ")"})
	edited := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Edited later", "body_md": "![o](" + from.URL + ")"})
	var done kbReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+from.ID+"/replace", map[string]any{"with_media_id": to.ID}, admin, http.StatusOK, &done)
	var current kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+edited.ID, nil, admin, http.StatusOK, &current)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+edited.ID, map[string]any{"updated_at": current.UpdatedAt, "body_md": current.BodyMD + " and more"}, admin, http.StatusOK, nil)

	var undone struct {
		Skipped []string `json:"skipped_article_ids"`
	}
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, admin, http.StatusOK, &undone)
	if len(undone.Skipped) != 1 || undone.Skipped[0] != edited.ID {
		t.Fatalf("skipped = %v, want the article edited since", undone.Skipped)
	}
	var after kbArticleView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+both.ID, nil, admin, http.StatusOK, &after)
	if want := "![o](" + from.URL + ") and ![n](" + to.URL + ")"; after.BodyMD != want {
		t.Fatalf("after undo body = %q, want %q", after.BodyMD, want)
	}
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+edited.ID, nil, admin, http.StatusOK, &after)
	if want := "![o](" + to.URL + ") and more"; after.BodyMD != want {
		t.Fatalf("edited article after undo = %q, want its own text %q", after.BodyMD, want)
	}
}

// A replacement across many articles and a save of one of them, at the same moment: the save is
// refused only as "changed elsewhere" (the replacement wrote it first), never by a deadlock.
func TestKBMediaReplaceAndSaveNeverDeadlock(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Replace Race", "admin@kb-replace-race.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	from := uploadKBMedia(t, api, admin, pngImage(153), nil)
	to := uploadKBMedia(t, api, admin, pngImage(154), nil)
	logo := uploadKBMedia(t, api, admin, pngImage(155), nil)
	var ids []string
	for i := range 30 {
		article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Article " + strconv.Itoa(i), "body_md": "![a](" + from.URL + ") ![l](" + logo.URL + ")"})
		ids = append(ids, article.ID)
	}
	sort.Strings(ids)
	last := ids[len(ids)-1]
	for round := range 20 {
		var current kbArticleView
		kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+last, nil, admin, http.StatusOK, &current)
		replace := newRequest(t, http.MethodPost, "/api/v1/kb/media/"+from.ID+"/replace", map[string]any{"with_media_id": to.ID}, admin, nil)
		save := newRequest(t, http.MethodPatch, "/api/v1/kb/articles/"+last, map[string]any{"updated_at": current.UpdatedAt, "title": "Round " + strconv.Itoa(round)}, admin, nil)
		var wait sync.WaitGroup
		var replaced, saved *httpResult
		start := make(chan struct{})
		wait.Add(2)
		go func() { defer wait.Done(); <-start; replaced = serveResult(api, replace) }()
		go func() {
			defer wait.Done()
			<-start
			time.Sleep(time.Duration(round%8) * time.Millisecond)
			saved = serveResult(api, save)
		}()
		close(start)
		wait.Wait()
		if replaced.code != http.StatusOK || (saved.code != http.StatusOK && !strings.Contains(string(saved.body), "changed_elsewhere")) {
			t.Fatalf("round %d: replace %d %s, save %d %s", round, replaced.code, replaced.body, saved.code, saved.body)
		}
		var done kbReplacementView
		if err := json.Unmarshal(replaced.body, &done); err != nil {
			t.Fatal(err)
		}
		kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, admin, http.StatusOK, nil)
	}
}
