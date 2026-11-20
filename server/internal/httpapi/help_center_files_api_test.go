package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/media/pdfsample"
)

// wantDownload checks a help center file answer: the bytes, as a download that cannot run.
func wantDownload(t *testing.T, response *httptest.ResponseRecorder, contentType, disposition string, size int) {
	t.Helper()
	header := response.Header()
	if response.Code != http.StatusOK || header.Get("Content-Type") != contentType || header.Get("Content-Disposition") != disposition ||
		header.Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(header.Get("Content-Security-Policy"), "sandbox") || response.Body.Len() != size {
		t.Fatalf("download status=%d headers=%v bytes=%d, want %s %q of %d bytes", response.Code, header, response.Body.Len(), contentType, disposition, size)
	}
}

// A file is public only while a published article (or a published translation in a shown language)
// of an open help center lists it; a draft, an unpublished article, a hidden language or another
// workspace gets the 404 of an id that does not exist.
func TestHelpCenterServesAFileOnlyWhileAPublishedArticleListsIt(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	site := helpCenterSite(api)
	admin := createSession(t, api, service, "Help Files", "admin@help-files.example.com")
	other := createSession(t, api, service, "Help Files Other", "admin@help-files-other.example.com")
	slug, otherSlug := workspaceSlug(t, api, admin), workspaceSlug(t, api, other)
	enableHelpCenter(t, api, admin, "Files Answers", true)
	enableHelpCenter(t, api, other, "Other Answers", true)
	base := "/help/" + slug
	missing := visit(site, base+"/files/00000000-0000-4000-8000-000000000001/nothing.pdf", "192.0.2.90")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown file status=%d", missing.Code)
	}
	wantMissing := func(path string) {
		t.Helper()
		if response := visit(site, path, "192.0.2.90"); response.Code != http.StatusNotFound || response.Body.String() != missing.Body.String() ||
			response.Header().Get("Content-Disposition") != "" {
			t.Fatalf("%s status=%d disposition=%q, want the 404 of an unknown id", path, response.Code, response.Header().Get("Content-Disposition"))
		}
	}

	form := pdfsample.PDF("Fill in the warranty form", 20<<20)
	file := uploadKBFile(t, api, admin, "Warranty form.pdf", form)
	sheet := uploadKBFile(t, api, admin, "parts list.csv", []byte("sku,name\nA-1,Kettle\n"))
	filePath, sheetPath := base+"/files/"+file.ID+"/warranty-form.pdf", base+"/files/"+sheet.ID+"/parts-list.csv"
	collection := kbCollection(t, api, admin, map[string]any{"name": "Returns"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns policy", "body_md": "Send it back."})
	articlePath := "/api/v1/kb/articles/" + article.ID
	listed := putKBAttachments(t, api, admin, articlePath, []string{file.ID}, http.StatusOK)
	wantMissing(filePath)

	var published kbArticleWithFiles
	kbCall(t, api, http.MethodPatch, articlePath, map[string]any{"status": "published", "updated_at": listed.UpdatedAt}, admin, http.StatusOK, &published)
	wantDownload(t, visit(site, filePath, "192.0.2.90"), "application/pdf", `attachment; filename*=UTF-8''Warranty%20form.pdf`, len(form))
	if head := serve(site, func() *http.Request {
		req := httptest.NewRequest(http.MethodHead, filePath, nil)
		req.RemoteAddr, req.Host = "192.0.2.90:4242", testOriginHost
		return req
	}()); head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD status=%d bytes=%d", head.Code, head.Body.Len())
	}
	// The page lists the file under the text with its type and size, by the address that downloads it.
	page := visit(site, fmt.Sprintf("%s/a/%d-%s", base, article.Number, article.Slug), "192.0.2.90")
	wantPage(t, page, http.StatusOK, []string{`href="` + filePath + `"`, "Warranty form.pdf", "PDF", "20 MB"}, []string{"parts list.csv"})

	// The draft of the published article lists the sheet and drops the form: neither changes until publish.
	putKBAttachments(t, api, admin, articlePath, []string{sheet.ID}, http.StatusOK)
	wantDownload(t, visit(site, filePath, "192.0.2.90"), "application/pdf", `attachment; filename*=UTF-8''Warranty%20form.pdf`, len(form))
	wantMissing(sheetPath)
	var withDraft kbArticleWithFiles
	kbCall(t, api, http.MethodGet, articlePath, nil, admin, http.StatusOK, &withDraft)
	kbCall(t, api, http.MethodPost, articlePath+"/draft/publish", map[string]any{"updated_at": withDraft.UpdatedAt}, admin, http.StatusOK, nil)
	wantDownload(t, visit(site, sheetPath, "192.0.2.90"), "text/csv", `attachment; filename*=UTF-8''parts%20list.csv`, 20)
	wantMissing(filePath)

	// A published translation lists files of its own: in a hidden language they stay private.
	kbCall(t, api, http.MethodPut, "/api/v1/help-center/languages", map[string]any{"languages": []map[string]any{{"locale": "fr", "visible": true}, {"locale": "pt", "visible": false}}}, admin, http.StatusOK, nil)
	small := pdfsample.PDF("Formulaire de garantie", 4096)
	other1 := uploadKBFile(t, api, admin, "Formulaire.pdf", small)
	other2 := uploadKBFile(t, api, admin, "Formulario.pdf", pdfsample.PDF("Formulario de garantia", 4096))
	for locale, id := range map[string]string{"fr": other1.ID, "pt": other2.ID} {
		kbCall(t, api, http.MethodPost, articlePath+"/translations", map[string]any{"locale": locale, "title": "Retours " + locale, "body_md": "x", "status": "published"}, admin, http.StatusCreated, nil)
		kbCall(t, api, http.MethodPut, articlePath+"/translations/"+locale+"/attachments", map[string]any{"media_ids": []string{id}}, admin, http.StatusOK, nil)
	}
	wantDownload(t, visit(site, base+"/files/"+other1.ID+"/formulaire.pdf", "192.0.2.90"), "application/pdf", `attachment; filename*=UTF-8''Formulaire.pdf`, len(small))
	wantMissing(base + "/files/" + other2.ID + "/formulario.pdf")

	// Another workspace's published file is not served below this help center, nor is this one below theirs.
	theirForm := pdfsample.PDF("Their form", 4096)
	theirs := uploadKBFile(t, api, other, "theirs.pdf", theirForm)
	theirCollection := kbCollection(t, api, other, map[string]any{"name": "Theirs"})
	theirArticle := kbArticle(t, api, other, map[string]any{"collection_id": theirCollection.ID, "title": "Theirs", "body_md": "x", "status": "published"})
	putKBAttachments(t, api, other, "/api/v1/kb/articles/"+theirArticle.ID, []string{theirs.ID}, http.StatusOK)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/articles/"+theirArticle.ID, nil, other, http.StatusOK, &withDraft)
	kbCall(t, api, http.MethodPost, "/api/v1/kb/articles/"+theirArticle.ID+"/draft/publish", map[string]any{"updated_at": withDraft.UpdatedAt}, other, http.StatusOK, nil)
	wantDownload(t, visit(site, "/help/"+otherSlug+"/files/"+theirs.ID+"/theirs.pdf", "192.0.2.90"), "application/pdf", `attachment; filename*=UTF-8''theirs.pdf`, len(theirForm))
	wantMissing(base + "/files/" + theirs.ID + "/theirs.pdf")
	if response := visit(site, "/help/"+otherSlug+"/files/"+sheet.ID+"/parts-list.csv", "192.0.2.90"); response.Code != http.StatusNotFound {
		t.Fatalf("this workspace's file below another help center status=%d", response.Code)
	}
}
