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
