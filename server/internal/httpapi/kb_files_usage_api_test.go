package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// A file an article lists is in use like an image its text shows: the library counts the article,
// lists it in the details and under "in use", filters files on their own, and keeps the file out of
// the trash until no article lists it.
func TestKBFileListedByAnArticleIsInUse(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB File Usage", "admin@kb-file-usage.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Returns"})
	form := uploadKBFile(t, api, admin, "Warranty form.pdf", []byte("%PDF-1.4\n"+strings.Repeat("x", 100)))
	loose := uploadKBFile(t, api, admin, "loose.csv", []byte("a,b\n1,2\n"))
	uploadKBMedia(t, api, admin, pngImage(42), nil)

	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns policy", "body_md": "Send it back."})
	path := "/api/v1/kb/articles/" + article.ID
	putKBAttachments(t, api, admin, path, []string{form.ID}, http.StatusOK)
	// The draft of a published article lists the same file a second time: it is still one article.
	live := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Warranty", "body_md": "Two years.", "status": "published"})
	putKBAttachments(t, api, admin, "/api/v1/kb/articles/"+live.ID, []string{form.ID}, http.StatusOK)

	files := kbMediaList(t, api, admin, "kind=file")
	if len(files.Items) != 2 {
		t.Fatalf("kind=file lists %d items, want the 2 files", len(files.Items))
	}
	for _, item := range files.Items {
		if item.Kind != "file" {
			t.Fatalf("kind=file lists a %s", item.Kind)
		}
		want := 0
		if item.ID == form.ID {
			want = 2
		}
		if item.Usage == nil || item.Usage.Articles != want {
			t.Fatalf("usage of %s = %+v, want %d articles", item.Name, item.Usage, want)
		}
	}
	inUse := kbMediaList(t, api, admin, "kind=file&usage=in_use")
	if len(inUse.Items) != 1 || inUse.Items[0].ID != form.ID {
		t.Fatalf("in use = %+v, want only the listed file", inUse.Items)
	}
	var details kbMediaView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+form.ID+"/details", nil, admin, http.StatusOK, &details)
	if len(details.Articles) != 2 || !details.Articles[0].Current || !details.Articles[1].Current {
		t.Fatalf("details articles = %+v, want the 2 articles that list it", details.Articles)
	}

	var moved kbMediaResults
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/trash", map[string]any{"ids": []string{form.ID, loose.ID}}, admin, http.StatusOK, &moved)
	if moved.status(form.ID) != "in_use" || moved.status(loose.ID) != "trashed" {
		t.Fatalf("trash = %+v, want the listed file in_use and the loose one trashed", moved)
	}
	putKBAttachments(t, api, admin, path, []string{}, http.StatusOK)
	kbCall(t, api, http.MethodDelete, "/api/v1/kb/articles/"+live.ID+"/draft?updated_at="+urlQuery(t, api, admin, live.ID), nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/trash", map[string]any{"ids": []string{form.ID}}, admin, http.StatusOK, &moved)
	if moved.status(form.ID) != "trashed" {
		t.Fatalf("trash after removal = %+v, want trashed", moved)
	}
}

// urlQuery is the escaped updated_at of an article, for a request that must name the version it saw.
func urlQuery(t *testing.T, api http.Handler, token, id string) string {
	t.Helper()
	var article kbArticleWithFiles
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+id, nil, token, http.StatusOK, &article)
	return strings.ReplaceAll(article.UpdatedAt, "+", "%2B")
}
