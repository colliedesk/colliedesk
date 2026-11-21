package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

// An autosave of a published translation keeps the words in its draft and adds no version: the help
// center keeps the published text, the next Save adds the version, and the teammate who autosaved is
// an author of the draft who never reviews it.
func TestAnAutosaveOfAPublishedTranslationWritesItsDraftWithoutAVersion(t *testing.T) {
	f := newLanguagesFixture(t)
	id := f.translated.ID
	writer := agentSession(t, f.api, f.admin, "writer@kb-translation-autosave.example.com")

	var saved kbDraftedTranslation
	kbCall(t, f.api, http.MethodPatch, kbTranslationPath(id, "fr"), map[string]any{"updated_at": f.frSeen, "autosave": true, "body_md": "Toute carte convient : mélèze."},
		writer, http.StatusOK, &saved)
	if !strings.HasPrefix(saved.BodyMD, "Toute carte convient : sapinette.") || saved.Draft == nil || saved.Draft.BodyMD != "Toute carte convient : mélèze." {
		t.Fatalf("autosave = %+v draft=%+v", saved.kbTranslationView, saved.Draft)
	}
	if got := kbTranslationVersionKinds(t, f.api, f.admin, id, "fr"); got != "create" {
		t.Fatalf("versions after an autosave = %s, want create", got)
	}
	wantPage(t, visit(f.site, f.base+"/fr/a/1-payer-par-carte", "198.51.100.93"), http.StatusOK, []string{"sapinette"}, []string{"mélèze"})

	response := kbCall(t, f.api, http.MethodPost, kbTranslationPath(id, "fr", "/reviews"), map[string]any{"reviewer_ids": []string{agentID(t, f.api, writer)}},
		f.admin, http.StatusBadRequest, nil)
	if code := errorCode(t, response); code != "invalid_request" {
		t.Fatalf("naming the teammate who autosaved the draft: code=%s", code)
	}
	response = kbCall(t, f.api, http.MethodPatch, kbTranslationPath(id, "fr"), map[string]any{"updated_at": saved.UpdatedAt, "autosave": true, "body_md": "x", "status": "draft"},
		f.admin, http.StatusBadRequest, nil)
	if code := errorCode(t, response); code != "invalid_request" {
		t.Fatalf("an autosave with a status: code=%s", code)
	}

	kbCall(t, f.api, http.MethodPatch, kbTranslationPath(id, "fr"), map[string]any{"updated_at": saved.UpdatedAt, "body_md": "Toute carte convient : mélèze."}, f.admin, http.StatusOK, nil)
	if got := kbTranslationVersionKinds(t, f.api, f.admin, id, "fr"); got != "draft,create" {
		t.Fatalf("versions after the Save = %s, want draft,create", got)
	}
}

// An autosave of a translation that was never published changes it in place and adds no version.
func TestAnAutosaveOfAnUnpublishedTranslationChangesItWithoutAVersion(t *testing.T) {
	f := newLanguagesFixture(t)
	id := f.translated.ID
	before := kbGetTranslation(t, f.api, f.admin, id, "es")
	var saved kbDraftedTranslation
	kbCall(t, f.api, http.MethodPatch, kbTranslationPath(id, "es"), map[string]any{"updated_at": before.UpdatedAt, "autosave": true, "title": "Pagar con una tarjeta"},
		f.admin, http.StatusOK, &saved)
	if saved.Title != "Pagar con una tarjeta" || saved.Status != "draft" || saved.HasDraft || saved.UpdatedAt == before.UpdatedAt {
		t.Fatalf("autosave of a draft translation = %+v", saved)
	}
	if got := kbTranslationVersionKinds(t, f.api, f.admin, id, "es"); got != "create" {
		t.Fatalf("versions after an autosave = %s, want create", got)
	}
}
