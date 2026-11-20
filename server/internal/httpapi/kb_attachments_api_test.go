package httpapi_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type kbAttachmentView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url"`
}

type kbArticleWithFiles struct {
	ID          string             `json:"id"`
	Status      string             `json:"status"`
	UpdatedAt   string             `json:"updated_at"`
	Attachments []kbAttachmentView `json:"attachments"`
	Draft       *struct {
		Attachments []kbAttachmentView `json:"attachments"`
	} `json:"draft"`
}

func attachmentNames(items []kbAttachmentView) string {
	names := make([]string, len(items))
	for index, item := range items {
		names[index] = item.Name
	}
	return strings.Join(names, ",")
}

func putKBAttachments(t *testing.T, api http.Handler, token, path string, ids []string, want int) kbArticleWithFiles {
	t.Helper()
	var article kbArticleWithFiles
	into := any(&article)
	if want != http.StatusOK {
		into = nil
	}
	kbCall(t, api, http.MethodPut, path+"/attachments", map[string]any{"media_ids": ids}, token, want, into)
	return article
}

// An article lists files in the order given; a published article lists them in its draft until the
// draft is published, and a discarded draft leaves the live list as it was.
func TestKBArticleListsFilesInOrderAndADraftCarriesItsOwnList(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Attach", "admin@kb-attach.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Returns"})
	form := uploadKBFile(t, api, admin, "Warranty form.pdf", []byte("%PDF-1.4\n"+strings.Repeat("x", 100)))
	sheet := uploadKBFile(t, api, admin, "parts.csv", []byte("sku,name\nA-1,Kettle\n"))

	draft := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Returns policy", "body_md": "Send it back."})
	path := "/api/v1/kb/articles/" + draft.ID
	listed := putKBAttachments(t, api, admin, path, []string{sheet.ID, form.ID}, http.StatusOK)
	if got := attachmentNames(listed.Attachments); got != "parts.csv,Warranty form.pdf" {
		t.Fatalf("unpublished article lists %q, want parts.csv,Warranty form.pdf", got)
	}
	if first := listed.Attachments[0]; first.ID != sheet.ID || first.ContentType != "text/csv" || first.Size != 20 || first.URL != "/api/v1/kb/media/"+sheet.ID {
		t.Fatalf("first attachment = %+v", first)
	}
	var read kbArticleWithFiles
	kbCall(t, api, http.MethodGet, path, nil, admin, http.StatusOK, &read)
	if got := attachmentNames(read.Attachments); got != "parts.csv,Warranty form.pdf" {
		t.Fatalf("read back %q", got)
	}

	published := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Warranty", "body_md": "Two years.", "status": "published"})
	livePath := "/api/v1/kb/articles/" + published.ID
	putKBAttachments(t, api, admin, livePath, []string{form.ID}, http.StatusOK)
	var withDraft kbArticleWithFiles
	kbCall(t, api, http.MethodGet, livePath, nil, admin, http.StatusOK, &withDraft)
	if len(withDraft.Attachments) != 0 || withDraft.Draft == nil || attachmentNames(withDraft.Draft.Attachments) != "Warranty form.pdf" {
		t.Fatalf("published article = live %v, draft %+v; want the file only in the draft", withDraft.Attachments, withDraft.Draft)
	}
	kbCall(t, api, http.MethodDelete, livePath+"/draft?updated_at="+url.QueryEscape(withDraft.UpdatedAt), nil, admin, http.StatusOK, nil)
	kbCall(t, api, http.MethodGet, livePath, nil, admin, http.StatusOK, &withDraft)
	if len(withDraft.Attachments) != 0 || withDraft.Draft != nil {
		t.Fatalf("after discard = %+v, want no files and no draft", withDraft)
	}
	putKBAttachments(t, api, admin, livePath, []string{form.ID, sheet.ID}, http.StatusOK)
	kbCall(t, api, http.MethodGet, livePath, nil, admin, http.StatusOK, &withDraft)
	var live kbArticleWithFiles
	kbCall(t, api, http.MethodPost, livePath+"/draft/publish", map[string]any{"updated_at": withDraft.UpdatedAt}, admin, http.StatusOK, &live)
	if got := attachmentNames(live.Attachments); got != "Warranty form.pdf,parts.csv" || live.Draft != nil {
		t.Fatalf("after publish = %q draft %+v, want Warranty form.pdf,parts.csv and no draft", got, live.Draft)
	}
	// An empty list removes every file from the article; the files stay in the library.
	putKBAttachments(t, api, admin, livePath, []string{}, http.StatusOK)
	kbCall(t, api, http.MethodGet, "/api/v1/kb/media/"+form.ID+"/details", nil, admin, http.StatusOK, nil)
}

// What an article cannot list is refused with how to fix it: an image, an unknown id, a file listed
// twice, more than 100 MB of files, and a token without kb_articles:update.
func TestKBArticleAttachmentsRefuseWhatTheyCannotList(t *testing.T) {
	api, service, pool, _ := testAPIWithStorage(t)
	admin := createSession(t, api, service, "KB Attach Limits", "admin@kb-attach-limits.example.com")
	collection := kbCollection(t, api, admin, map[string]any{"name": "Manuals"})
	article := kbArticle(t, api, admin, map[string]any{"collection_id": collection.ID, "title": "Manuals", "body_md": "See below."})
	path := "/api/v1/kb/articles/" + article.ID

	image := uploadKBImage(t, api, admin, kbPNG, http.StatusCreated)
	notAFile := serve(api, newRequest(t, http.MethodPut, path+"/attachments", map[string]any{"media_ids": []string{image.ID}}, admin, nil))
	if notAFile.Code != http.StatusNotFound || !strings.Contains(notAFile.Body.String(), image.ID) || !strings.Contains(notAFile.Body.String(), "kind=file") {
		t.Fatalf("an image status=%d body=%s, want 404 naming the id and the files list", notAFile.Code, notAFile.Body.String())
	}
	putKBAttachments(t, api, admin, path, []string{"00000000-0000-4000-8000-000000000001"}, http.StatusNotFound)

	var ids []string
	for index := range 5 {
		file := uploadKBFile(t, api, admin, "manual-"+string(rune('a'+index))+".pdf", []byte("%PDF-1.4\n"+strings.Repeat(string(rune('a'+index)), 100)))
		ids = append(ids, file.ID)
	}
	// Five files of 21 MB each come to 105 MB, over the 100 MB an article lists.
	if _, err := pool.Exec(context.Background(), `UPDATE kb_media SET size=21*1024*1024 WHERE id=ANY($1::uuid[])`, ids); err != nil {
		t.Fatal(err)
	}
	tooMuch := serve(api, newRequest(t, http.MethodPut, path+"/attachments", map[string]any{"media_ids": ids}, admin, nil))
	if tooMuch.Code != http.StatusRequestEntityTooLarge || errorCode(t, tooMuch) != "too_large" || !strings.Contains(tooMuch.Body.String(), "100 MiB") {
		t.Fatalf("105 MB status=%d body=%s, want 413 naming 100 MiB", tooMuch.Code, tooMuch.Body.String())
	}
	if listed := putKBAttachments(t, api, admin, path, ids[:4], http.StatusOK); len(listed.Attachments) != 4 {
		t.Fatalf("84 MB lists %d files, want 4", len(listed.Attachments))
	}
	twice := serve(api, newRequest(t, http.MethodPut, path+"/attachments", map[string]any{"media_ids": []string{ids[0], ids[0]}}, admin, nil))
	if twice.Code != http.StatusBadRequest {
		t.Fatalf("a file twice status=%d body=%s, want 400", twice.Code, twice.Body.String())
	}

	read := issueToken(t, api, admin, "Reader", "read").Token
	refused(t, api, http.MethodPut, path+"/attachments", map[string]any{"media_ids": ids[:1]}, read, "kb_articles:update")
}

// Another workspace's file cannot be listed: it is the same 404 as an id that does not exist.
func TestKBArticleAttachmentsStayInTheirWorkspace(t *testing.T) {
	api, service, _, _ := testAPIWithStorage(t)
	alpha := createSession(t, api, service, "KB Attach Alpha", "admin@kb-attach-alpha.example.com")
	beta := createSession(t, api, service, "KB Attach Beta", "admin@kb-attach-beta.example.com")
	theirs := uploadKBFile(t, api, beta, "secret.pdf", []byte("%PDF-1.4\n"+strings.Repeat("x", 100)))
	collection := kbCollection(t, api, alpha, map[string]any{"name": "Docs"})
	article := kbArticle(t, api, alpha, map[string]any{"collection_id": collection.ID, "title": "Docs", "body_md": "x"})
	putKBAttachments(t, api, alpha, "/api/v1/kb/articles/"+article.ID, []string{theirs.ID}, http.StatusNotFound)
	unknown := serve(api, newRequest(t, http.MethodPut, "/api/v1/kb/articles/"+article.ID+"/attachments",
		map[string]any{"media_ids": []string{"00000000-0000-4000-8000-000000000001"}}, alpha, nil))
	foreign := serve(api, newRequest(t, http.MethodPut, "/api/v1/kb/articles/"+article.ID+"/attachments",
		map[string]any{"media_ids": []string{theirs.ID}}, alpha, nil))
	if foreign.Code != unknown.Code || strings.ReplaceAll(errorMessage(t, foreign), theirs.ID, "ID") != strings.ReplaceAll(errorMessage(t, unknown), "00000000-0000-4000-8000-000000000001", "ID") {
		t.Fatalf("another workspace's file = %s, unknown id = %s; want the same answer", foreign.Body.String(), unknown.Body.String())
	}
	// Beta's article cannot be changed from Alpha either.
	betaCollection := kbCollection(t, api, beta, map[string]any{"name": "Beta docs"})
	betaArticle := kbArticle(t, api, beta, map[string]any{"collection_id": betaCollection.ID, "title": "Beta", "body_md": "x"})
	putKBAttachments(t, api, alpha, "/api/v1/kb/articles/"+betaArticle.ID, []string{}, http.StatusNotFound)
}
