package httpapi_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/media/pdfsample"
	"github.com/colliedesk/colliedesk/server/internal/store"
)

// AI retrieval reads the text of the files a published article lists as part of that article, each
// file headed with its name, its type and the article that lists it. A file only an unpublished article
// lists reaches nothing.
func TestAIRetrievalFindsAnArticleByTheTextOfItsFiles(t *testing.T) {
	api, service, pool := testAPIWithPool(t)
	admin := createSession(t, api, service, "Retrieval Files", "admin@retrieval-files.example.com")
	workspace := workspaceID(t, api, admin)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Returns"}).ID
	bigKB(t, api, admin, collection)
	form := uploadKBFile(t, api, admin, "Warranty form.pdf", pdfsample.PDF("Fill in the zephyrine warranty form", 4096))
	hidden := uploadKBFile(t, api, admin, "notes.txt", []byte("quillwort is only in a draft"))
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": "Returns policy", "body_md": "Send it back.", "status": "published"})
	putKBAttachments(t, api, admin, "/api/v1/kb/articles/"+article.ID, []string{form.ID}, http.StatusOK)
	var draft kbArticleWithFiles
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+article.ID, nil, admin, http.StatusOK, &draft)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+article.ID+"/draft/publish", map[string]any{"updated_at": draft.UpdatedAt}, admin, http.StatusOK, nil)
	unpublished := kbArticle(t, api, admin, map[string]any{"collection_id": collection, "title": "Draft notes", "body_md": "Not yet."})
	putKBAttachments(t, api, admin, "/api/v1/kb/articles/"+unpublished.ID, []string{hidden.ID}, http.StatusOK)
	buildAIRetrieval(t, pool)

	got := aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "zephyrine", KBLimit: 5})
	if len(got) == 0 || got[0].KBArticleID != article.ID {
		t.Fatalf("query for a word only in the PDF = %v, want the article that lists it first", retrievedTitles(got))
	}
	if !strings.Contains(got[0].Text, "## Warranty form (PDF) in Returns policy") || !strings.Contains(got[0].Text, "zephyrine warranty form") {
		t.Errorf("retrieved text = %q, want the PDF's text under its name, type and article", got[0].Text)
	}
	for _, doc := range aiRetrieve(t, pool, workspace, store.AIRetrievalQuery{Text: "quillwort", KBLimit: 5}) {
		if strings.Contains(doc.Text, "quillwort") {
			t.Errorf("the file of an unpublished article reached AI in %q", doc.Title)
		}
	}
}
