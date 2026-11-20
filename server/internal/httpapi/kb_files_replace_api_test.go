package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/media/pdfsample"
)

type kbFileReplacementView struct {
	kbReplacementView
	InPlace bool `json:"in_place"`
}

type kbFileView struct {
	kbMediaView
	DeletedAt *string `json:"deleted_at"`
}

// A file is replaced in place: it keeps its id and its address, every article that lists it shows the
// new bytes at once, and the old bytes wait in the trash as the earlier version. Undo moves them back.
func TestKBFileReplacedInPlaceKeepsItsAddressAndUndoes(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "KB File Replace", "admin@kb-file-replace.example.com")
	enableHelpCenter(t, api, admin, "Replace Answers", true)
	base := "/help/" + workspaceSlug(t, api, admin)
	first, second := pdfsample.PDF("First edition", 4096), pdfsample.PDF("Second edition", 9000)
	old := uploadKBFile(t, api, admin, "Warranty form.pdf", first)
	next := uploadKBFile(t, api, admin, "Warranty form v2.pdf", second)
	collection := kbCollection(t, api, admin, map[string]any{"name": "Returns"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns policy", "body_md": "Send it back."})
	listed := putKBAttachments(t, api, admin, "/api/v1/kb/articles/"+article.ID, []string{old.ID}, http.StatusOK)
	kbCall(t, api, http.MethodPatch, "/api/v1/kb/articles/"+article.ID, map[string]any{"status": "published", "updated_at": listed.UpdatedAt}, admin, http.StatusOK, nil)
	public := base + "/files/" + old.ID + "/warranty-form.pdf"

	editor := customToken(t, api, admin, "Editor", "kb_articles:read", "kb_articles:update", "kb_media:read", "kb_media:update")
	refused(t, api, http.MethodPost, "/api/v1/kb/media/"+old.ID+"/replace", map[string]any{"with_media_id": next.ID}, editor, "kb_articles:publish")

	var done kbFileReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+old.ID+"/replace", map[string]any{"with_media_id": next.ID}, admin, http.StatusOK, &done)
	if !done.InPlace || done.From != old.ID || done.To != next.ID || len(done.ArticleIDs) != 0 {
		t.Fatalf("replacement = %+v, want in place with no article rewritten", done)
	}
	var got kbFileView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+old.ID+"/details", nil, admin, http.StatusOK, &got)
	if got.URL != old.URL || got.Size != int64(len(second)) || got.Name != "Warranty form v2.pdf" || got.DeletedAt != nil {
		t.Fatalf("replaced file = %+v, want the old address with the new bytes (%d)", got, len(second))
	}
	var earlier kbFileView
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+next.ID+"/details", nil, admin, http.StatusOK, &earlier)
	if earlier.Size != int64(len(first)) || earlier.Name != "Warranty form.pdf" || earlier.DeletedAt == nil {
		t.Fatalf("earlier version = %+v, want the old bytes (%d) in the trash", earlier, len(first))
	}
	wantDownload(t, visit(site, public, "192.0.2.91"), "application/pdf", `attachment; filename*=UTF-8''Warranty%20form%20v2.pdf`, len(second))
	private := serve(api, newRequest(t, http.MethodGet, old.URL, nil, admin, nil))
	wantDownload(t, private, "application/pdf", `attachment; filename*=UTF-8''Warranty%20form%20v2.pdf`, len(second))
	if cache := private.Header().Get("Cache-Control"); cache != "private, no-cache" {
		t.Errorf("a file's bytes change under its address, so Cache-Control = %q must revalidate", cache)
	}

	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+old.ID+"/details", nil, admin, http.StatusOK, &got)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+next.ID+"/details", nil, admin, http.StatusOK, &earlier)
	if got.Size != int64(len(first)) || got.Name != "Warranty form.pdf" || earlier.Size != int64(len(second)) || earlier.DeletedAt != nil {
		t.Fatalf("after undo file = %+v, other = %+v; want the old bytes back and the new upload out of the trash", got, earlier)
	}
	wantDownload(t, visit(site, public, "192.0.2.91"), "application/pdf", `attachment; filename*=UTF-8''Warranty%20form.pdf`, len(first))
}

// A file is replaced only by a file no article lists, so no other article loses what it lists; the
// other workspace's files are never found.
func TestKBFileReplaceRefusesAListedFileAndAnotherWorkspace(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB File Replace Guard", "admin@kb-file-replace-guard.example.com")
	other := createSession(t, api, service, "KB File Replace Other", "admin@kb-file-replace-other.example.com")
	old := uploadKBFile(t, api, admin, "a.txt", []byte("first"))
	listed := uploadKBFile(t, api, admin, "b.txt", []byte("second"))
	foreign := uploadKBFile(t, api, other, "c.txt", []byte("third"))
	collection := kbCollection(t, api, admin, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Lists b", "body_md": "Text."})
	putKBAttachments(t, api, admin, "/api/v1/kb/articles/"+article.ID, []string{listed.ID}, http.StatusOK)

	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+old.ID+"/replace", map[string]any{"with_media_id": listed.ID}, admin, http.StatusBadRequest, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+old.ID+"/replace", map[string]any{"with_media_id": foreign.ID}, admin, http.StatusNotFound, nil)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+foreign.ID+"/replace", map[string]any{"with_media_id": old.ID}, admin, http.StatusNotFound, nil)
	var done kbFileReplacementView
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media/"+listed.ID+"/replace", map[string]any{"with_media_id": old.ID}, admin, http.StatusOK, &done)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/media-replacements/"+done.ID+"/undo", nil, other, http.StatusNotFound, nil)
}
