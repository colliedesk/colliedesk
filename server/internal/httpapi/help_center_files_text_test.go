package httpapi_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/media/pdfsample"
)

// The text of a file a published article lists finds that article in the help center search, and the
// article's Markdown copy and llms.txt list its files by their public addresses. A file only a draft or
// an unpublished article lists finds nothing.
func TestHelpCenterSearchAndTextCopiesIncludeAnArticlesFiles(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Help File Text", "admin@help-file-text.example.com")
	slug := workspaceSlug(t, api, admin)
	enableHelpCenter(t, api, admin, "File Text Answers", true)
	base := "/help/" + slug

	form := uploadKBFile(t, api, admin, "Warranty form.pdf", pdfsample.PDF("Fill in the zephyrine warranty form", 4096))
	hidden := uploadKBFile(t, api, admin, "notes.txt", []byte("quillwort is only in a draft"))
	collection := kbCollection(t, api, admin, map[string]any{"name": "Returns"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns policy", "body_md": "Send it back.", "status": "published"})
	articlePath := "/api/v1/kb/articles/" + article.ID
	var draft kbArticleWithFiles
	kbCall(t, api, http.MethodGet, articlePath, nil, admin, http.StatusOK, &draft)
	// A published article's list is written to its draft until the draft is published.
	putKBAttachments(t, api, admin, articlePath, []string{form.ID}, http.StatusOK)
	kbCall(t, api, http.MethodGet, articlePath, nil, admin, http.StatusOK, &draft)
	kbCall(t, api, http.MethodPost, articlePath+"/draft/publish", map[string]any{"updated_at": draft.UpdatedAt}, admin, http.StatusOK, nil)
	unpublished := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Draft notes", "body_md": "Not yet."})
	putKBAttachments(t, api, admin, "/api/v1/kb/articles/"+unpublished.ID, []string{hidden.ID}, http.StatusOK)

	wantPage(t, visit(site, base+"/search?q=zephyrine", "192.0.2.95"), http.StatusOK, []string{"1 result for “zephyrine”", "Returns policy"}, nil)
	wantPage(t, visit(site, base+"/search?q=quillwort", "192.0.2.95"), http.StatusOK, []string{"No results for “quillwort”"}, []string{"Draft notes"})

	address := "https://" + testOriginHost + base
	fileURL := address + "/files/" + form.ID + "/warranty-form.pdf"
	markdown := visit(site, fmt.Sprintf("%s/a/%d-%s.md", base, article.Number, article.Slug), "192.0.2.95")
	wantBody := "# Returns policy\n\nSend it back.\n\n## Downloads\n\n- [Warranty form.pdf](" + fileURL + ") (PDF, 4 KB)\n"
	if markdown.Code != http.StatusOK || markdown.Body.String() != wantBody {
		t.Fatalf("markdown status=%d body=%q, want %q", markdown.Code, markdown.Body.String(), wantBody)
	}
	wantPage(t, visit(site, base+"/llms.txt", "192.0.2.95"), http.StatusOK, []string{
		fmt.Sprintf("- [Returns policy](%s/a/%d-%s.md)\n  - [Warranty form.pdf](%s) (PDF, 4 KB)\n", address, article.Number, article.Slug, fileURL),
	}, []string{"notes.txt"})
}
