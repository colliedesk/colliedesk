package httpapi_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/colliedesk/colliedesk/server/internal/media/pdfsample"
)

// kbTranslationWithFiles is a translation as the API answers it, with the files of its draft.
type kbTranslationWithFiles struct {
	UpdatedAt   string             `json:"updated_at"`
	HasDraft    bool               `json:"has_draft"`
	Attachments []kbAttachmentView `json:"attachments"`
	Draft       *struct {
		Attachments []kbAttachmentView `json:"attachments"`
	} `json:"draft"`
}

// A teammate who may edit but not publish changes the files and the text of a published translation:
// both wait in its draft, the help center keeps serving neither the new file nor the new image, and
// they go public only when someone who may publish publishes the draft.
func TestAPublishedTranslationsNewFilesAndImagesStayPrivateUntilItsDraftIsPublished(t *testing.T) {
	f := newLanguagesFixture(t)
	id := f.translated.ID
	editor := issueTokenWith(t, f.api, f.admin, map[string]any{"label": "Editor",
		"permissions": []string{"kb_articles:read", "kb_articles:update", "kb_media:read"}}).Token
	form := pdfsample.PDF("Formulaire de garantie", 4096)
	file := uploadKBFile(t, f.api, f.admin, "Formulaire.pdf", form)
	image := uploadKBImage(t, f.api, f.admin, otherPNG(57), http.StatusCreated)
	filePath, imagePath := f.base+"/files/"+file.ID+"/formulaire.pdf", f.base+"/images/"+image.ID
	private := func(step string) {
		t.Helper()
		for _, path := range []string{filePath, imagePath} {
			if response := visit(f.site, path, "198.51.100.94"); response.Code != http.StatusNotFound {
				t.Fatalf("%s: %s status=%d, want 404", step, path, response.Code)
			}
		}
	}

	var listed kbTranslationWithFiles
	kbCall(t, f.api, http.MethodPut, kbTranslationPath(id, "fr", "/attachments"), map[string]any{"media_ids": []string{file.ID}, "updated_at": f.frSeen},
		editor, http.StatusOK, &listed)
	if len(listed.Attachments) != 0 || !listed.HasDraft || listed.Draft == nil || attachmentNames(listed.Draft.Attachments) != "Formulaire.pdf" {
		t.Fatalf("listed = %+v", listed)
	}
	var saved kbTranslationWithFiles
	kbCall(t, f.api, http.MethodPatch, kbTranslationPath(id, "fr"), map[string]any{"updated_at": listed.UpdatedAt, "body_md": "Voir le formulaire.\n\n![Carte](" + image.URL + ")"},
		editor, http.StatusOK, &saved)
	if saved.Draft == nil || attachmentNames(saved.Draft.Attachments) != "Formulaire.pdf" {
		t.Fatalf("a text save dropped the draft's files: %+v", saved)
	}
	private("in the draft")
	refused(t, f.api, http.MethodPost, kbTranslationPath(id, "fr", "/draft/publish"), map[string]any{"updated_at": saved.UpdatedAt}, editor, "kb_articles:publish")
	private("after the refused publish")

	var published kbTranslationWithFiles
	kbCall(t, f.api, http.MethodPost, kbTranslationPath(id, "fr", "/draft/publish"), map[string]any{"updated_at": saved.UpdatedAt}, f.admin, http.StatusOK, &published)
	if published.HasDraft || attachmentNames(published.Attachments) != "Formulaire.pdf" {
		t.Fatalf("published = %+v", published)
	}
	wantDownload(t, visit(f.site, filePath, "198.51.100.94"), "application/pdf", `attachment; filename*=UTF-8''Formulaire.pdf`, len(form))
	if response := visit(f.site, imagePath, "198.51.100.94"); response.Code != http.StatusOK {
		t.Fatalf("published image status=%d", response.Code)
	}

	// Restoring the first version puts it in the draft, so an editor may do it; discarding drops it.
	response := postWithKey(t, f.api, editor, kbTranslationPath(id, "fr", "/versions/1/restore"), "restore-by-editor", map[string]any{"updated_at": published.UpdatedAt})
	if response.Code != http.StatusOK {
		t.Fatalf("restore by an editor status=%d body=%s", response.Code, response.Body.String())
	}
	var restored kbTranslationWithFiles
	decode(t, response, &restored)
	kbCall(t, f.api, http.MethodDelete, kbTranslationPath(id, "fr", "/draft?updated_at="+url.QueryEscape(restored.UpdatedAt)), nil, editor, http.StatusOK, nil)
}

// Another workspace can neither publish nor discard a translation's draft, and the draft stays.
func TestAnotherWorkspaceCannotPublishOrDiscardATranslationsDraft(t *testing.T) {
	f := newLanguagesFixture(t)
	id := f.translated.ID
	other := createSession(t, f.api, f.service, "Help Languages Other", "admin@help-languages-other.example.com")
	var saved kbTranslationWithFiles
	kbCall(t, f.api, http.MethodPatch, kbTranslationPath(id, "fr"), map[string]any{"updated_at": f.frSeen, "body_md": "Brouillon."}, f.admin, http.StatusOK, &saved)
	for _, call := range []struct{ method, path string }{
		{http.MethodPost, kbTranslationPath(id, "fr", "/draft/publish")},
		{http.MethodDelete, kbTranslationPath(id, "fr", "/draft?updated_at="+url.QueryEscape(saved.UpdatedAt))},
	} {
		var body any
		if call.method == http.MethodPost {
			body = map[string]any{"updated_at": saved.UpdatedAt}
		}
		if response := request(t, f.api, call.method, call.path, body, other, nil); response.Code != http.StatusNotFound {
			t.Errorf("other workspace %s %s status=%d body=%s, want 404", call.method, call.path, response.Code, response.Body.String())
		}
	}
	if got := kbGetTranslation(t, f.api, f.admin, id, "fr"); !got.HasDraft || got.Draft == nil || got.Draft.BodyMD != "Brouillon." {
		t.Fatalf("the draft after another workspace's calls = %+v", got.Draft)
	}
}
