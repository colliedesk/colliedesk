package httpapi_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestAnArticleCountsTheVotesFreshdeskHadBeforeAnyNewOne(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Vote Import", "admin@vote-import.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Vote help", true)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	imported := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Imported guide", "status": "published", "body_md": "a"})
	fresh := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Fresh guide", "status": "published", "body_md": "b"})
	if _, err := pool.Exec(context.Background(), `UPDATE kb_articles SET source_meta='{"freshdesk_id": 9, "thumbs_up": 12, "thumbs_down": 3}' WHERE id=$1`, imported.ID); err != nil {
		t.Fatal(err)
	}

	wantPage(t, visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, imported.Number, imported.Slug), "192.0.2.60"), http.StatusOK,
		[]string{"Was this article helpful?", "12 out of 15 found this helpful"}, nil)
	// No vote yet: the question, without a count.
	wantPage(t, visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, fresh.Number, fresh.Slug), "192.0.2.60"), http.StatusOK,
		[]string{"Was this article helpful?"}, []string{"found this helpful"})
}

func TestImportedVotesCountOnlyTheTranslationsReadersCanSee(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Vote Translations", "admin@vote-translations.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "Vote help", true)
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{
		{"locale": "fr", "visible": true}, {"locale": "de", "visible": true}, {"locale": "es", "visible": true}}}, admin, http.StatusOK, nil)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Guides"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Imported guide", "status": "published", "body_md": "a"})
	for locale, status := range map[string]string{"fr": "published", "de": "draft", "es": "published"} {
		kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/translations",
			map[string]any{"locale": locale, "title": "Guide " + locale, "body_md": "x", "status": status}, admin, http.StatusCreated, nil)
	}
	for _, statement := range []string{
		`UPDATE kb_articles SET source_meta='{"freshdesk_id": 9, "thumbs_up": 12, "thumbs_down": 3}' WHERE id=$1`,
		`UPDATE kb_article_translations SET source_meta='{"freshdesk_id": 10, "thumbs_up": 5, "thumbs_down": 0}' WHERE article_id=$1 AND locale='fr'`,
		// A draft and a translation in the trash were never shown with their thumbs: they do not count.
		`UPDATE kb_article_translations SET source_meta='{"freshdesk_id": 11, "thumbs_up": 100, "thumbs_down": 0}' WHERE article_id=$1 AND locale='de'`,
		`UPDATE kb_article_translations SET source_meta='{"freshdesk_id": 12, "thumbs_up": 100, "thumbs_down": 100}', deleted_at=now() WHERE article_id=$1 AND locale='es'`,
	} {
		if _, err := pool.Exec(context.Background(), statement, article.ID); err != nil {
			t.Fatal(err)
		}
	}
	wantPage(t, visit(site, fmt.Sprintf("/help/%s/a/%d-%s", slug, article.Number, article.Slug), "192.0.2.64"), http.StatusOK,
		[]string{"17 out of 20 found this helpful"}, nil)

	// Kept for agents, the article leaves the help center, but the team still reads every vote it brought in.
	if _, err := pool.Exec(context.Background(), `UPDATE kb_collections SET visibility='agents' WHERE id=$1`, collection.ID); err != nil {
		t.Fatal(err)
	}
	var votes kbVotesView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID+"/votes", nil, admin, http.StatusOK, &votes)
	if votes.Imported.Helpful != 17 || votes.Imported.NotHelpful != 3 {
		t.Fatalf("imported votes of an agents only article = %+v, want 17 helpful and 3 not", votes.Imported)
	}
}
